package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// A reminder occurrence is valid only while its trigger still names the same
// instant and its guard still applies. Result notices have no reminder trigger.
const currentNoticeTriggerSQL = `(n.trigger_id LIKE 'run:%' OR EXISTS (
 SELECT 1 FROM jsonb_array_elements(coalesce(nullif(w.document->'triggers','null'::jsonb),'[]'::jsonb)) t
 WHERE t->>'id'=n.trigger_id AND t->>'active'='true'
 AND nullif(t->>'nextAt','')::timestamptz=n.due_at
 AND (coalesce(t->>'guard','')='' OR t->>'guard'=w.status)))`

// Persist invalidation so an old occurrence cannot reappear in the pinned
// list or be retried by another dispatcher after a due-time edit.
func invalidateObsoleteNoticesTx(ctx context.Context, tx pgx.Tx, selector string, id string) error {
	// The trigger edit already advances the owner's revision. Delivery must
	// not acquire the owner lock after the notice lock or hold it across Send.
	_, err := tx.Exec(ctx, `UPDATE workspace_notices n SET dismissed_at=now() FROM work_items w
 WHERE (w.owner_id,w.id)=(n.owner_id,n.thing_id) AND `+selector+`
 AND n.dismissed_at IS NULL AND NOT `+currentNoticeTriggerSQL, id)
	return err
}

// RunReminders evaluates explicit times and acknowledged conditions. Natural
// language conditions stay unresolved until there is evidence, never become
// arbitrary keyword triggers or age-based nags.
func (s *Store) RunReminders(ctx context.Context, logger *slog.Logger) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			_ = s.CleanupBlobs(ctx)
			if err := s.CheckReminders(ctx, now); err != nil && ctx.Err() == nil {
				logger.Warn("reminder evaluation failed")
			}
		}
	}
}
func (s *Store) CheckReminders(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, "SELECT owner_id::text FROM workspace_owners")
	if err != nil {
		return err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var id memory.ID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range owners {
		scope := memory.Scope{OwnerID: id, PrincipalID: "worker", IsOwner: true}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(id)); err != nil {
				return err
			}
			if err := invalidateObsoleteNoticesTx(ctx, tx, "n.owner_id=$1", string(id)); err != nil {
				return err
			}
			settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(id))
			if err != nil {
				return err
			}
			items, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND status NOT IN ('done','cancelled','dropped','promoted')", string(id))
			if err != nil {
				return err
			}
			changed := false
			loc, err := time.LoadLocation(settings.Timezone)
			if err != nil {
				return err
			}
			clock, err := time.Parse("15:04", settings.DailyReviewAt)
			if err != nil {
				return err
			}
			local := now.In(loc)
			due := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
			if !now.Before(due) {
				var pending int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM capture_candidates WHERE owner_id=$1 AND state='pending'", string(id)).Scan(&pending); err != nil {
					return err
				}
				tag, err := tx.Exec(ctx, "INSERT INTO workspace_reviews(owner_id,due_at,pending_count) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(id), due, pending)
				if err != nil {
					return err
				}
				changed = tag.RowsAffected() > 0
			}
			for _, item := range items {
				if item.Kind == "task" && settings.FollowUps {
					for _, trigger := range item.Triggers {
						if !trigger.Active || trigger.NextAt == "" {
							continue
						}
						at, err := time.Parse(time.RFC3339, trigger.NextAt)
						if err != nil || now.Before(at) {
							continue
						}
						if trigger.Guard != "" && trigger.Guard != item.Status {
							continue
						}
						tag, err := tx.Exec(ctx, "INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", string(id), item.ID, trigger.ID, at, trigger.Description)
						if err != nil {
							return err
						}
						changed = changed || tag.RowsAffected() > 0
					}
				}
				if item.Kind != "idea" || !settings.WakeIdeas || !item.RemindersOn || item.Status != "shelved" {
					continue
				}
				if item.Wake != nil {
					if item.Wake.SnoozedUntil != "" {
						at, err := time.Parse(time.RFC3339, item.Wake.SnoozedUntil)
						if err == nil && now.Before(at) {
							continue
						}
					}
					continue
				}
				for i := range item.Conditions {
					condition := &item.Conditions[i]
					at, err := time.Parse(time.RFC3339, condition.DueAt)
					if condition.Kind == "time" && err == nil && !now.Before(at) {
						condition.Met = true
						condition.MetAt = now.UTC().Format(time.RFC3339)
					}
					if !condition.Met {
						continue
					}
					if condition.Kind != "time" && condition.MetBy == nil {
						continue
					}
					item.Status = "awakened"
					item.Wake = &workspace.Wake{At: now.UTC().Format(time.RFC3339), Reason: condition.Description, ConditionID: condition.ID}
					item.Version++
					item.UpdatedAt = stamp()
					item.Evolution = append(item.Evolution, workspace.Revision{At: stamp(), By: "system", Summary: "唤醒：" + condition.Description})
					if err := saveItem(ctx, tx, scope, item); err != nil {
						return err
					}
					changed = true
					break
				}
			}
			if changed {
				_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(id))
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Undo records suppression for this occurrence, rather than changing restored
// business content (which would invalidate earlier actions' content hashes).
// Reusing the existing occurrence key preserves restart and deletion behavior.
func suppressRestoredRemindersTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, current, restored workspace.Item, now time.Time) error {
	if current.Kind != "task" || current.Status != "done" || !oneOf(restored.Status, "todo", "doing", "waiting") {
		return nil
	}
	for _, trigger := range restored.Triggers {
		if !trigger.Active || trigger.NextAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, trigger.NextAt)
		if err != nil || !at.Before(now.Add(-time.Hour)) {
			continue
		}
		// Preserve an existing real notice and every delivery/dismissal field.
		// Only a newly inserted suppression placeholder is hidden from views.
		_, err = tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason,delivered)
			VALUES($1,$2,$3,$4,$5,'{"_suppressed":true,"_suppressionOnly":true}'::jsonb)
			ON CONFLICT(owner_id,thing_id,trigger_id,due_at) DO UPDATE
			SET delivered=workspace_notices.delivered || '{"_suppressed":true}'::jsonb
			WHERE workspace_notices.dismissed_at IS NULL`, string(scope.OwnerID), restored.ID, trigger.ID, at, trigger.Description)
		if err != nil {
			return err
		}
	}
	return nil
}
