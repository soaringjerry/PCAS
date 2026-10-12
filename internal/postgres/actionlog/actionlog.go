// Package actionlog is the storage of the record of one action of the owner
// or an agent: the changes that the action made, kept so that the action can
// be undone, and the mark of a check-only turn.
package actionlog

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

type ActionLogKey struct{}

type ActionLog struct{ ID, source, TurnID, summary, UndoOf string }

type ActionChange struct {
	Table     string          `json:"table"`
	ID        string          `json:"id"`
	Before    json.RawMessage `json:"before"`
	AfterHash *string         `json:"afterHash"`
}

func WithActionLog(ctx context.Context, id, source, turnID, summary string) context.Context {
	return context.WithValue(ctx, ActionLogKey{}, ActionLog{ID: id, source: source, TurnID: turnID, summary: summary})
}

func BeginActionLogTx(ctx context.Context, tx pgx.Tx) error {
	if ctx.Value(ActionLogKey{}) == nil {
		return nil
	}
	_, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','[]',true)")
	return err
}

func FlushActionLog(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	// Retain the audit metadata, including undone_at, after the undo window.
	// Run even when this transaction collected no new document changes.
	if _, err := tx.Exec(ctx, "UPDATE action_log SET changes='[]'::jsonb,expired_at=now() WHERE owner_id=$1 AND expired_at IS NULL AND created_at<now()-interval '30 days'", string(scope.OwnerID)); err != nil {
		return err
	}
	log, ok := ctx.Value(ActionLogKey{}).(ActionLog)
	if !ok {
		return nil
	}
	var changes []byte
	if err := tx.QueryRow(ctx, "SELECT current_setting('pcas.action_changes')::jsonb").Scan(&changes); err != nil {
		return err
	}
	var entries []ActionChange
	if err := json.Unmarshal(changes, &entries); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','',true)"); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,turn_id,summary,changes) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), log.ID, log.source, kernel.NullString(log.TurnID), log.summary, changes)
	if err != nil {
		return err
	}
	// Written separately so the pre-053 legacy fixtures keep inserting the
	// columns they have; only an undo carries the marker.
	if log.UndoOf != "" {
		if _, err := tx.Exec(ctx, "UPDATE action_log SET undo_of=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), log.ID, log.UndoOf); err != nil {
			return err
		}
	}
	return recordSmokeActionTx(ctx, tx, scope, log.ID, entries)
}

type smokeKey struct{}

func WithSmoke(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, smokeKey{}, id)
}

func SmokeID(ctx context.Context) string {
	id, _ := ctx.Value(smokeKey{}).(string)
	return id
}

// Only server-collected changes may choose these SQL table names.
func SmokeTable(table string) bool {
	return kernel.OneOf(table, "work_items", "work_documents", "agent_runs", "training_samples")
}

func SmokeDocumentHashTx(ctx context.Context, tx pgx.Tx, owner, table, id string) (*string, error) {
	if !SmokeTable(table) {
		return nil, memory.ErrInvalid
	}
	var hash string
	err := tx.QueryRow(ctx, "SELECT encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+table+" WHERE owner_id=$1 AND id=$2", owner, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &hash, err
}

func recordSmokeActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, actionID string, changes []ActionChange) error {
	id := SmokeID(ctx)
	if id == "" {
		return nil
	}
	owner := string(scope.OwnerID)
	var order int64
	if err := tx.QueryRow(ctx, "SELECT action_order FROM action_log WHERE owner_id=$1 AND id=$2", owner, actionID).Scan(&order); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO desk_smoke_actions(owner_id,action_id,smoke_id) VALUES($1,$2,$3)", owner, actionID, id); err != nil {
		return err
	}
	for _, c := range changes {
		hash, err := SmokeDocumentHashTx(ctx, tx, owner, c.Table, c.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO desk_smoke_changes(owner_id,smoke_id,table_name,record_id,before_document,after_hash,action_order)
   VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(owner_id,smoke_id,table_name,record_id)
   DO UPDATE SET after_hash=excluded.after_hash,action_order=excluded.action_order`, owner, id, c.Table, c.ID, c.Before, hash, order); err != nil {
			return err
		}
	}
	return nil
}
