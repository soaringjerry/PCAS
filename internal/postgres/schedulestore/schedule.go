package schedulestore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/currentstate/schedule"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var _ workspace.ScheduleReader = (*Schedules)(nil)

// ListSchedule reads the remembered dates and the dated tasks of a range.
// schedule.Build places them.
func (s *Schedules) ListSchedule(ctx context.Context, scope memory.Scope, q workspace.ScheduleQuery) (workspace.Schedule, error) {
	out := workspace.Schedule{From: q.From, To: q.To, Days: []workspace.ScheduleDay{}, Unclear: []workspace.ScheduleEntry{}, Overdue: []workspace.ScheduleEntry{}}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		out.Timezone = settings.Timezone
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return err
		}
		_, _, end, err := schedule.Window(q, loc)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT d.id::text,d.kind,d.at,d.recurrence,d.title,d.time_note,d.claim_id::text,coalesce(nullif(d.original_text,''),c.value #>> '{}'),d.date_only,d.schedule_rule
 FROM deadlines d JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 WHERE d.owner_id=$1 AND r.state='active' AND rv.state='active' AND cl.retired='' AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text) AND claim_source_is_current(r.owner_id,r.id,r.version,now())
 AND (d.kind IN('recurring','unclear') OR d.at<$2) ORDER BY d.id`, string(scope.OwnerID), end)
		if err != nil {
			return err
		}
		deadlines := []schedule.Deadline{}
		for rows.Next() {
			var d schedule.Deadline
			e := &d.Entry
			if err = rows.Scan(&e.Source.DeadlineID, &e.Kind, &d.At, &d.Recurrence, &e.Title, &e.TimeNote, &e.Source.MemoryID, &e.OriginalText, &e.DateOnly, &d.Rule); err != nil {
				rows.Close()
				return err
			}
			e.ID = e.Source.DeadlineID
			e.Source.Kind = "deadline"
			deadlines = append(deadlines, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		tasks, err := kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status NOT IN('done','cancelled') AND (due_at IS NOT NULL OR scheduled_at IS NOT NULL) ORDER BY id`, string(scope.OwnerID))
		if err != nil {
			return err
		}
		out, err = schedule.Build(q, settings.Timezone, deadlines, tasks)
		return err
	})
	return out, err
}

// ListInProgress reads the open undated tasks; schedule.InProgress picks.
func (s *Schedules) ListInProgress(ctx context.Context, scope memory.Scope) (workspace.InProgress, error) {
	out := workspace.InProgress{Items: []workspace.Item{}}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		// The document is checked again in schedule.InProgress for old imported
		// rows with absent SQL columns.
		items, err := kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status NOT IN('done','cancelled') AND due_at IS NULL AND scheduled_at IS NULL AND NOT coalesce((document->>'urgent')::boolean,false) ORDER BY coalesce(nullif(document->>'updatedAt','')::timestamptz,updated_at) DESC,id`, string(scope.OwnerID))
		if err != nil {
			return err
		}
		out = schedule.InProgress(items)
		return nil
	})
	return out, err
}
