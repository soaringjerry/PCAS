package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type sourcePolicyUndoAbsenceKey struct{}

// Only the server undo path can restore absence semantics. An HTTP Revoke
// request always records explicit denial, including when no grant existed.
func sourcePolicyRevokeIsExplicit(ctx context.Context) bool {
	absence, _ := ctx.Value(sourcePolicyUndoAbsenceKey{}).(bool)
	return !absence
}

func recordSourcePolicyActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, before *memory.SourceAuthorization, after memory.SourceAuthorization) error {
	return recordContextActionTx(ctx, tx, "source_authorizations", string(after.ID), asJSON(before), after.Revision)
}

func recordSourceScopeActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, before, after memory.SourceScopeResult) error {
	return recordContextActionTx(ctx, tx, "source_scope_revisions", string(after.Source.ID), asJSON(before), after.Revision)
}

func recordContextActionTx(ctx context.Context, tx pgx.Tx, table, id string, before json.RawMessage, revision int) error {
	var buffer string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.action_changes',true),'')").Scan(&buffer); err != nil {
		return err
	}
	if buffer == "" {
		return nil
	} // No action requested by this caller.
	var changes []actionChange
	if err := json.Unmarshal([]byte(buffer), &changes); err != nil {
		return err
	}
	hash := fmt.Sprint(revision)
	for i := range changes {
		if changes[i].Table == table && changes[i].ID == id {
			changes[i].AfterHash = &hash
			_, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", string(asJSON(changes)))
			return err
		}
	}
	changes = append(changes, actionChange{Table: table, ID: id, Before: before, AfterHash: &hash})
	_, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", string(asJSON(changes)))
	return err
}
