package postgres

import (
	"context"
	"crypto/rand"
	"math"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) reserveBackgroundCost(ctx context.Context, j worker.Job, cost float64) error {
	return s.reserveModelCost(ctx, j.OwnerID, cost, &j)
}
func (s *Store) reserveModelCost(ctx context.Context, owner memory.ID, cost float64, j *worker.Job) error {
	_, err := s.reserveModelCostID(ctx, owner, cost, j)
	return err
}

// Each invocation settles its own row, including concurrent calls without a job.
func (s *Store) reserveModelCostID(ctx context.Context, owner memory.ID, cost float64, j *worker.Job) (string, error) {
	var id string
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return "", memory.ErrInvalid
	}
	scope := memory.Scope{OwnerID: owner, PrincipalID: "worker", IsOwner: true}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(owner)); err != nil {
			return err
		}
		stage := ""
		if j != nil {
			stage = backgroundStage(j.Stage)
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':budget:'||$1,0))", stage); err != nil {
				return err
			}
			if err := backgroundHourlyTx(ctx, tx, stage, stage+"_hourly_limit"); err != nil {
				return err
			}
		}
		var jobID any
		if j != nil {
			jobID = string(j.ID)
			if err := lockJob(ctx, tx, *j); err != nil {
				return err
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM background_usage WHERE job_id=$1)", string(j.ID)).Scan(&exists); err != nil {
				return err
			}
			if exists && j.Attempts != 1 && cost > 0 && backgroundHourlyBudgets[backgroundStage(j.Stage)] == 0 {
				return &worker.JobError{Code: "model_call_failed"}
			}
		}
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(owner))
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return err
		}
		now := time.Now().In(loc)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		var spent float64
		if err := tx.QueryRow(ctx, "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1 AND created_at>=$2),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1 AND created_at>=$2),0)", string(owner), start).Scan(&spent); err != nil {
			return err
		}
		if spent+cost > settings.DailyBudget {
			if j == nil {
				return memory.ErrUnavailable
			}
			jitter, err := rand.Int(rand.Reader, big.NewInt(int64(budgetJitter)+1))
			if err != nil {
				return err
			}
			return &worker.JobError{Code: "budget_deferred", Until: nextBudgetDay(now, loc).Add(time.Duration(jitter.Int64())), NoAttempt: true}
		}
		// Migration verification can still exercise foreground use on the old
		// schema. Resolve the actual relation instead of a same-named public table.
		var staged bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='background_usage'::regclass AND attname='stage' AND NOT attisdropped)").Scan(&staged); err != nil {
			return err
		}
		if !staged {
			return tx.QueryRow(ctx, "INSERT INTO background_usage(owner_id,job_id,reserved_cost) VALUES($1,$2,$3) RETURNING id::text", string(owner), jobID, cost).Scan(&id)
		}
		return tx.QueryRow(ctx, "INSERT INTO background_usage(owner_id,job_id,reserved_cost,stage) VALUES($1,$2,$3,$4) RETURNING id::text", string(owner), jobID, cost, stage).Scan(&id)
	})
	return id, err
}

// A disconnected caller or failed result transaction must not leave a completed
// invocation's maximum reservation in the daily budget. Settlement is separate
// from business writes and does not authorize an automatic model retry.
func (s *Store) settleModelCost(ctx context.Context, owner memory.ID, id string, cost float64) error {
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return memory.ErrInvalid
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(settleCtx, "UPDATE background_usage SET reserved_cost=$3 WHERE owner_id=$1 AND id=$2", string(owner), id, cost)
	return err
}

// Deletion uses an opaque invocation ID derived from owner, run and creation
// time. Run IDs can coincide across owners or be reused after deletion. Settle
// either location under the deletion lock without restoring deleted text.
// Billing survives result-lease loss; creation time fences a reused run ID.
func (s *Store) settleRunCost(ctx context.Context, owner memory.ID, run workspace.Run, cost float64) error {
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return pgx.BeginFunc(settleCtx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(settleCtx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(owner)); err != nil {
			return err
		}
		if _, err := tx.Exec(settleCtx, "UPDATE agent_runs SET reserved_cost=$4,document=jsonb_set(document,'{cost}',to_jsonb($4::numeric)) WHERE owner_id=$1 AND id=$2 AND document->>'createdAt'=$3", string(owner), run.ID, run.CreatedAt, cost); err != nil {
			return err
		}
		_, err := tx.Exec(settleCtx, "UPDATE background_usage SET reserved_cost=$3 WHERE owner_id=$1 AND id=md5($1::uuid::text || ':' || $2::uuid::text || ':' || $4::text)::uuid", string(owner), run.ID, cost, run.CreatedAt)
		return err
	})
}

const budgetJitter = 10 * time.Minute

func nextBudgetDay(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, time.UTC)
	next := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	wallDate := func(at time.Time) time.Time {
		at = at.In(loc)
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	}
	// A midnight DST jump can normalize Date to the preceding evening. Advance
	// to the next calendar day; at a repeated midnight choose its first instant.
	for wallDate(next).Before(date) {
		next = next.Add(time.Second)
	}
	for wallDate(next.Add(-time.Second)).Equal(date) {
		next = next.Add(-time.Second)
	}
	return next
}
