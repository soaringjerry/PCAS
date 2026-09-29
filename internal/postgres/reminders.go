package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

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
