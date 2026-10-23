package workstore

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/workspace/reminder"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type ConditionSignal = extraction.Signal

func (s *Work) PendingConditions(ctx context.Context, scope memory.Scope) ([]map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT i.document FROM work_items i JOIN workspace_owners w USING(owner_id) WHERE i.owner_id=$1 AND i.kind='idea' AND i.status='shelved' AND (i.document->>'remindersOn')::boolean AND (w.settings->>'wakeIdeas')::boolean ORDER BY i.updated_at DESC LIMIT $2`, string(scope.OwnerID), reminder.PendingIdeas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ideas := []workspace.Item{}
	for rows.Next() {
		var item workspace.Item
		if err := rows.Scan(&item); err != nil {
			return nil, err
		}
		ideas = append(ideas, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reminder.PendingConditions(ideas), nil
}

// ApplySignalsTx wakes the ideas that a source gives a sign of.
// reminder.ApplySignals decides.
func (s *Work) ApplySignalsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, source memory.Source, signals []ConditionSignal) error {
	return reminder.ApplySignals(ctx, signalUnit{tx: tx, scope: scope}, source, signals, time.Now())
}

// signalUnit is the storage of the signs of one source.
type signalUnit struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (u signalUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u signalUnit) AddSignalNotice(ctx context.Context, item, condition string, at time.Time, reason string, source memory.Source) (bool, error) {
	tag, err := u.tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason,source_id,source_version) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, string(u.scope.OwnerID), item, condition, at, reason, string(source.ID), source.Version)
	return err == nil && tag.RowsAffected() > 0, err
}

func (u signalUnit) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, u.tx, u.scope, item)
}
