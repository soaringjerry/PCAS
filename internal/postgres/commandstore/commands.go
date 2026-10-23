package commandstore

import (
	"context"
	"errors"
	"time"

	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/workspace/command"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// CommandTx applies one workspace command in the transaction tx.
func (s *Commands) CommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	return command.Apply(ctx, &commandUnit{store: s, tx: tx, scope: scope}, c, time.Now())
}

// The wire format is deliberately a small explicit command vocabulary. Client
// supplied run outputs, costs, provenance, and processing states are not trusted.

// deleteItemTx removes an item for good: its reminders, documents and agent
// runs go with it, other to-dos stop depending on it, and the receipts memory
// kept of its changes are deleted like any other material.
func (s *Commands) deleteItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item) error {
	var receipt memory.Ref
	err := tx.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
		WHERE s.owner_id=$1 AND s.connector='actions' AND s.external_id=$2 AND r.state='active'`, string(scope.OwnerID), item.ID).Scan(&receipt.ID, &receipt.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		receipt.Kind = memory.SourceKind
		if err := s.DeleteRecordsTx(ctx, tx, scope, memory.DeleteRequest{Targets: []memory.Ref{receipt}}); err != nil {
			return err
		}
	}
	if err := itemstore.RemoveItemDependency(ctx, tx, scope.OwnerID, item.ID); err != nil {
		return err
	}
	// Undo keeps earlier versions of the item, its documents, its agent runs
	// and the samples drawn from them. A deletion empties those too: the
	// trigger does it for the item and its documents once this setting names
	// the owner, and the runs and samples are named here.
	var previous string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.expire_actions',true),'')").Scan(&previous); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", string(scope.OwnerID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE action_log SET changes='[]'::jsonb,expired_at=now()
		WHERE owner_id=$1 AND expired_at IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(changes) c WHERE
		 (c->>'table'='agent_runs' AND c->>'id' IN (SELECT id::text FROM agent_runs WHERE owner_id=$1 AND thing_id=$2))
		 OR (c->>'table'='training_samples' AND c->>'id' IN (SELECT t.id::text FROM training_samples t JOIN agent_runs r ON (r.owner_id,r.id)=(t.owner_id,t.run_id) WHERE r.owner_id=$1 AND r.thing_id=$2))
		 OR (c->>'table'='work_items' AND c->>'id'=$2::text))`, string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	// Samples drawn from this item's agent runs refer to those runs and go first.
	if _, err := tx.Exec(ctx, "DELETE FROM training_samples WHERE owner_id=$1 AND run_id IN (SELECT id FROM agent_runs WHERE owner_id=$1 AND thing_id=$2)", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if err := itemstore.DeleteItem(ctx, tx, scope.OwnerID, item.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", previous)
	return err
}
