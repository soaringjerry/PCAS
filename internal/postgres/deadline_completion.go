package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Completion is an annotation on one memory version. Record only its three
// keys so undo never restores unrelated scope fields or the memory's contents.
type deadlineCompletion struct {
	Version int                        `json:"version"`
	Mark    map[string]json.RawMessage `json:"mark"`
}

func deadlineCompletionFromScope(version int, data []byte) (deadlineCompletion, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return deadlineCompletion{}, err
	}
	out := deadlineCompletion{Version: version, Mark: map[string]json.RawMessage{}}
	for _, key := range []string{"deadline_completed", "deadline_completed_version", "deadline_completed_at"} {
		if v, ok := fields[key]; ok {
			out.Mark[key] = v
		}
	}
	return out, nil
}

func deadlineCompletionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, claim string) (deadlineCompletion, error) {
	var version int
	var data []byte
	err := tx.QueryRow(ctx, `SELECT c.version,c.scope FROM memory_records r JOIN claim_revisions c
 ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=$1 AND r.id=$2 AND r.state='active' FOR UPDATE OF r,c`, string(scope.OwnerID), claim).Scan(&version, &data)
	if err != nil {
		return deadlineCompletion{}, err
	}
	return deadlineCompletionFromScope(version, data)
}

func (v deadlineCompletion) hash() string {
	return fmt.Sprintf("%x", sha256.Sum256(asJSON(v)))
}

func completeDeadlineTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, claim string, version int) error {
	before, err := deadlineCompletionTx(ctx, tx, scope, claim)
	if err != nil {
		return err
	}
	if before.Version != version {
		return memory.ErrConflict
	}
	if string(before.Mark["deadline_completed"]) == "true" && string(before.Mark["deadline_completed_version"]) == fmt.Sprint(version) {
		return nil // An already-completed memory does not create another action.
	}
	var data []byte
	if err = tx.QueryRow(ctx, `UPDATE claim_revisions SET scope=scope||jsonb_build_object(
 'deadline_completed',true,'deadline_completed_version',version,'deadline_completed_at',clock_timestamp())
 WHERE owner_id=$1 AND claim_id=$2 AND version=$3 RETURNING scope`, string(scope.OwnerID), claim, version).Scan(&data); err != nil {
		return err
	}
	after, err := deadlineCompletionFromScope(version, data)
	if err != nil {
		return err
	}
	if ctx.Value(actionLogKey{}) == nil {
		return nil
	}
	hash := after.hash()
	change := actionChange{Table: "deadline_completion", ID: claim, Before: asJSON(before), AfterHash: &hash}
	_, err = tx.Exec(ctx, `SELECT set_config('pcas.action_changes',
 (current_setting('pcas.action_changes')::jsonb||$1::jsonb)::text,true)`, asJSON([]actionChange{change}))
	return err
}

func checkDeadlineCompletionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, change actionChange) error {
	var before deadlineCompletion
	if !memory.ID(change.ID).Valid() || strictJSON(change.Before, &before) != nil || before.Version < 1 || before.Mark == nil || change.AfterHash == nil {
		return memory.ErrInvalid
	}
	current, err := deadlineCompletionTx(ctx, tx, scope, change.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspace.ErrChangedSince
	}
	if err != nil {
		return err
	}
	if current.Version != before.Version || current.hash() != *change.AfterHash {
		return workspace.ErrChangedSince
	}
	return nil
}

func restoreDeadlineCompletionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, change actionChange) error {
	var before deadlineCompletion
	if err := json.Unmarshal(change.Before, &before); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE claim_revisions SET scope=(scope-
 ARRAY['deadline_completed','deadline_completed_version','deadline_completed_at'])||$4::jsonb
 WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(scope.OwnerID), change.ID, before.Version, asJSON(before.Mark))
	return err
}
