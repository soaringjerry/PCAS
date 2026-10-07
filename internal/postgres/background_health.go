package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func stageEventTx(ctx context.Context, tx pgx.Tx, owner memory.ID, stage, outcome, reason string, count int) error {
	if outcome == "failure" {
		// The job's slot is handed back in this same write; that is not a success.
		if _, err := tx.Exec(ctx, "SELECT set_config('pcas.stage_failed',$1,true)", stage); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, "INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,$2,$3,$4,$5)", nullString(string(owner)), stage, outcome, reason, count)
	return err
}

func backgroundFailureReason(err error) string {
	var job *worker.JobError
	if errors.As(err, &job) && job.Code != "" {
		return job.Code
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return "postgres_" + pg.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return fmt.Sprintf("%T", err)
}

// The owner's row is held while the user's own command commits. Scheduling
// gives way and returns on the next tick, so one busy owner does not end the
// pass; the missed tick is still recorded with its cause.
func (s *Store) scheduleYielded(ctx context.Context, owner memory.ID, stage string, err error) bool {
	if !statusScheduleBusy(err) {
		return false
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := pgx.BeginFunc(persist, s.pool, func(tx pgx.Tx) error {
		return stageEventTx(persist, tx, owner, stage, "failure", "schedule_"+backgroundFailureReason(err), 1)
	}); e != nil {
		slog.ErrorContext(ctx, "background event storage failed", "stage", stage, "reason", backgroundFailureReason(e))
	}
	return true
}

func (s *Store) recordScheduleFailure(ctx context.Context, stage string, err error) {
	reason := backgroundFailureReason(err)
	slog.WarnContext(ctx, "background schedule failed", "stage", stage, "reason", reason)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := pgx.BeginFunc(persist, s.pool, func(tx pgx.Tx) error { return stageEventTx(persist, tx, "", stage, "failure", "schedule_"+reason, 1) }); e != nil {
		slog.ErrorContext(ctx, "background event storage failed", "stage", stage, "reason", backgroundFailureReason(e))
	}
}

func (s *Store) BackgroundHealth(ctx context.Context, scope memory.Scope) (json.RawMessage, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	var out json.RawMessage
	err := s.pool.QueryRow(ctx, `WITH stages AS (SELECT key AS stage,value::int AS budget FROM jsonb_each_text($2::jsonb)),
 totals AS (SELECT stage,outcome,sum(count) AS n FROM background_stage_events WHERE (owner_id=$1 OR owner_id IS NULL) AND at>now()-interval '1 hour' AND reason NOT IN('reorganize_deadlines_withdrawn','reorganize_requirements_withdrawn') GROUP BY stage,outcome)
 SELECT jsonb_build_object('stages',coalesce(jsonb_agg(jsonb_build_object('stage',s.stage,'hourlyBudget',s.budget,
 'success',coalesce((SELECT n FROM totals WHERE stage=s.stage AND outcome='success'),0),
 'deferred',coalesce((SELECT n FROM totals WHERE stage=s.stage AND outcome='deferred'),0),
 'failure',coalesce((SELECT n FROM totals WHERE stage=s.stage AND outcome='failure'),0),
 'overflow',coalesce((SELECT n FROM totals WHERE stage=s.stage AND outcome='overflow'),0),
 'deferredBy',coalesce((SELECT jsonb_object_agg(reason,n) FROM (SELECT reason,sum(count) AS n FROM background_stage_events e WHERE (e.owner_id=$1 OR e.owner_id IS NULL) AND e.stage=s.stage AND e.outcome='deferred' AND e.at>now()-interval '1 hour' GROUP BY reason) why),'{}'::jsonb),
 'lastFailure',coalesce((SELECT reason FROM background_stage_events e WHERE (e.owner_id=$1 OR e.owner_id IS NULL) AND e.stage=s.stage AND e.outcome='failure' ORDER BY at DESC,id DESC LIMIT 1),''),
 'withdrawn',jsonb_build_object(
 'deadlines',coalesce((SELECT sum(count) FROM background_stage_events e WHERE e.owner_id=$1 AND e.stage=s.stage AND e.reason='reorganize_deadlines_withdrawn' AND e.at>now()-interval '1 hour'),0),
 'requirements',coalesce((SELECT sum(count) FROM background_stage_events e WHERE e.owner_id=$1 AND e.stage=s.stage AND e.reason='reorganize_requirements_withdrawn' AND e.at>now()-interval '1 hour'),0)),
 'calls',(SELECT count(*) FROM background_usage b WHERE b.stage=s.stage AND b.created_at>now()-interval '1 hour')) ORDER BY s.stage),'[]'::jsonb),
 'overflow',coalesce((SELECT jsonb_agg(jsonb_build_object('stage',stage,'reason',reason,'count',n)) FROM (
 SELECT stage,reason,sum(count) AS n FROM background_stage_events WHERE owner_id=$1 AND outcome='overflow' AND at>now()-interval '1 hour' GROUP BY stage,reason) excess),'[]'::jsonb)) FROM stages s`, string(scope.OwnerID), asJSON(backgroundHourlyBudgets)).Scan(&out)
	return out, err
}

func (s *Store) warnLongDeferrals(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `UPDATE background_job_deferrals SET warned=true WHERE NOT warned AND started_at<now()-interval '30 minutes'
 RETURNING job_id::text,started_at`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var started time.Time
		if err := rows.Scan(&id, &started); err != nil {
			return err
		}
		slog.WarnContext(ctx, "background job deferred over 30 minutes", "job_id", id, "deferred_since", started)
	}
	return rows.Err()
}
