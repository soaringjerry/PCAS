package workspacestore

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/reminder"
)

// A reminder occurrence is valid only while its trigger still names the same
// instant and its guard still applies. Result notices have no reminder trigger.
const CurrentNoticeTriggerSQL = `(n.trigger_id LIKE 'run:%' OR EXISTS (
 SELECT 1 FROM jsonb_array_elements(coalesce(nullif(w.document->'triggers','null'::jsonb),'[]'::jsonb)) t
 WHERE t->>'id'=n.trigger_id AND t->>'active'='true'
 AND nullif(t->>'nextAt','')::timestamptz=n.due_at
 AND (coalesce(t->>'guard','')='' OR t->>'guard'=w.status)))`

// Persist invalidation so an old occurrence cannot reappear in the pinned
// list or be retried by another dispatcher after a due-time edit.
func InvalidateObsoleteNoticesTx(ctx context.Context, tx pgx.Tx, selector string, id string) error {
	// The trigger edit already advances the owner's revision. Delivery must
	// not acquire the owner lock after the notice lock or hold it across Send.
	_, err := tx.Exec(ctx, `UPDATE workspace_notices n SET dismissed_at=now() FROM work_items w
 WHERE (w.owner_id,w.id)=(n.owner_id,n.thing_id) AND `+selector+`
 AND n.dismissed_at IS NULL AND NOT `+CurrentNoticeTriggerSQL, id)
	return err
}

// RunReminders evaluates explicit times and acknowledged conditions. Natural
// language conditions stay unresolved until there is evidence, never become
// arbitrary keyword triggers or age-based nags.
func (s *Workspace) RunReminders(ctx context.Context, logger *slog.Logger) error {
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

// CheckReminders records what is due for each owner at now. reminder.Check
// decides; reminderUnit reads and writes under the owner's lock.
func (s *Workspace) CheckReminders(ctx context.Context, now time.Time) error {
	rows, err := s.Pool.Query(ctx, "SELECT owner_id::text FROM workspace_owners")
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
		if err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(id)); err != nil {
				return err
			}
			if err := InvalidateObsoleteNoticesTx(ctx, tx, "n.owner_id=$1", string(id)); err != nil {
				return err
			}
			return reminder.Check(ctx, reminderUnit{tx: tx, scope: memory.Scope{OwnerID: id, PrincipalID: "worker", IsOwner: true}}, now)
		}); err != nil {
			return err
		}
	}
	return nil
}

// reminderUnit is the storage of the reminders of one owner.
type reminderUnit struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (u reminderUnit) owner() string { return string(u.scope.OwnerID) }

func (u reminderUnit) Settings(ctx context.Context) (workspace.Settings, error) {
	return kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", u.owner())
}

func (u reminderUnit) OpenItems(ctx context.Context) ([]workspace.Item, error) {
	return kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM work_items WHERE owner_id=$1 AND status NOT IN ('done','cancelled','dropped','promoted')", u.owner())
}

func (u reminderUnit) PendingCandidates(ctx context.Context) (int, error) {
	var pending int
	err := u.tx.QueryRow(ctx, "SELECT count(*) FROM capture_candidates WHERE owner_id=$1 AND state='pending'", u.owner()).Scan(&pending)
	return pending, err
}

func (u reminderUnit) AddReview(ctx context.Context, due time.Time, pending int) error {
	_, err := u.tx.Exec(ctx, "INSERT INTO workspace_reviews(owner_id,due_at,pending_count) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", u.owner(), due, pending)
	return err
}

func (u reminderUnit) Noticed(ctx context.Context, item, trigger string) (bool, error) {
	var once bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspace_notices WHERE owner_id=$1 AND thing_id=$2 AND trigger_id=$3)", u.owner(), item, trigger).Scan(&once)
	return once, err
}

func (u reminderUnit) AddNotice(ctx context.Context, item, trigger string, at time.Time, reason string) error {
	_, err := u.tx.Exec(ctx, "INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", u.owner(), item, trigger, at, reason)
	return err
}

func (u reminderUnit) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, u.tx, u.scope, item)
}

// Undo records suppression for this occurrence, rather than changing restored
// business content (which would invalidate earlier actions' content hashes).
// Reusing the existing occurrence key preserves restart and deletion behavior.
func SuppressRestoredRemindersTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, current, restored workspace.Item, now time.Time) error {
	for _, trigger := range reminder.Suppressed(current, restored, now) {
		if err := SuppressNoticeTx(ctx, tx, scope, restored, trigger); err != nil {
			return err
		}
	}
	return nil
}

// SuppressNoticeTx keeps the occurrence of a passed trigger from being sent.
// It preserves an existing real notice and every delivery or dismissal
// field; only a newly inserted placeholder is hidden from views.
func SuppressNoticeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, trigger workspace.Trigger) error {
	at, err := time.Parse(time.RFC3339, trigger.NextAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason,delivered)
		VALUES($1,$2,$3,$4,$5,'{"_suppressed":true,"_suppressionOnly":true}'::jsonb)
		ON CONFLICT(owner_id,thing_id,trigger_id,due_at) DO UPDATE
		SET delivered=workspace_notices.delivered || '{"_suppressed":true}'::jsonb
		WHERE workspace_notices.dismissed_at IS NULL`, string(scope.OwnerID), item.ID, trigger.ID, at, trigger.Description)
	return err
}
