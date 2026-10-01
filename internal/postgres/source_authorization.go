package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Public source mutations use the same owner gate, command deduplication ledger
// and undo action buffer as workspace commands. Mutations called by a secretary
// already participate in its transaction and must not open a second writer.
func (s *Store) beginSourceCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, requestID string, hash []byte) (int64, bool, error) {
	if err := requireOwner(scope); err != nil {
		return 0, false, err
	}
	if !memory.ID(requestID).Valid() {
		return 0, false, memory.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return 0, false, err
	}
	if err := s.ensureOwner(ctx, tx, scope); err != nil {
		return 0, false, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, "SELECT revision FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)).Scan(&revision); err != nil {
		return 0, false, err
	}
	var priorHash []byte
	err := tx.QueryRow(ctx, "SELECT request_hash FROM workspace_commands WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), requestID).Scan(&priorHash)
	if err == nil {
		if !bytes.Equal(priorHash, hash) {
			return 0, false, memory.ErrConflict
		}
		return revision, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	return revision, false, nil
}

func finishSourceCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, requestID string, hash []byte, revision int64) error {
	if err := flushActionLog(ctx, tx, scope); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO workspace_commands(owner_id,request_id,request_hash,revision) VALUES($1,$2,$3,$4)", string(scope.OwnerID), requestID, hash, revision+1)
	return err
}

func sourceActionUndoableTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, requestID string) (bool, error) {
	var undoable bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM action_log WHERE owner_id=$1 AND id=$2 AND undone_at IS NULL AND expired_at IS NULL AND created_at>=now()-interval '30 days')`, string(scope.OwnerID), requestID).Scan(&undoable)
	return undoable, err
}

func (s *Store) SourceAuthorizations(ctx context.Context, scope memory.Scope, id memory.ID) ([]memory.SourceAuthorization, error) {
	out := []memory.SourceAuthorization{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !id.Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.id=$2 AND r.state='active')", string(scope.OwnerID), string(id)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return memory.ErrNotFound
		}
		rows, err := tx.Query(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND source_id=$2 ORDER BY created_at,id", string(scope.OwnerID), string(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			policy, err := scanSourcePolicy(rows)
			if err != nil {
				return err
			}
			out = append(out, policy)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) SetSourceAuthorization(ctx context.Context, scope memory.Scope, in memory.SourceAuthorizationRequest) (memory.SourceAuthorizationResult, error) {
	var out memory.SourceAuthorizationResult
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(in.RequestID).Valid() || !validSourceRef(in.Source) || in.ExpectedPolicyRevision < 0 {
		return out, memory.ErrInvalid
	}
	hash := sha256.Sum256(asJSON(in))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		revision, duplicate, err := s.beginSourceCommandTx(ctx, tx, scope, in.RequestID, hash[:])
		if err != nil {
			return err
		}
		if duplicate {
			if _, err := lockSourceMutationTx(ctx, tx, scope.OwnerID, in.Source); err != nil {
				return err
			}
			// The action receipt binds a partial owner selection to its original
			// canonical destination. Never resolve it against a changed route.
			var policyID string
			err = tx.QueryRow(ctx, `SELECT c->>'id' FROM action_log l CROSS JOIN LATERAL jsonb_array_elements(l.changes) c WHERE l.owner_id=$1 AND l.id=$2 AND l.undone_at IS NULL AND l.expired_at IS NULL AND c->>'table'='source_authorizations'`, string(scope.OwnerID), in.RequestID).Scan(&policyID)
			if errors.Is(err, pgx.ErrNoRows) {
				return memory.ErrConflict
			}
			if err != nil {
				return err
			}
			out.Authorization, err = scanSourcePolicy(tx.QueryRow(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), policyID))
			if errors.Is(err, pgx.ErrNoRows) {
				return memory.ErrConflict
			}
			if err != nil {
				return err
			}
			// An old receipt never replays its mutation over a later grant,
			// revoke, undo, scope change, replacement, or deletion.
			if out.Authorization.SourceID != in.Source.ID || out.Authorization.Revision != in.ExpectedPolicyRevision+1 || !recipientSelectionMatches(in.Recipient, out.Authorization.Recipient) || in.Purpose != "" && out.Authorization.Purpose != in.Purpose || in.Scope != (memory.HardScope{}) && out.Authorization.Scope != in.Scope || out.Authorization.Revoked != in.Revoke || in.PolicyID != "" && in.PolicyID != out.Authorization.ID {
				return memory.ErrConflict
			}
			if !in.Revoke {
				actual, err := s.canonicalSourceRecipientTx(ctx, tx, scope, in.Recipient)
				if err != nil || actual != out.Authorization.Recipient {
					return memory.ErrConflict
				}
			}
			out.Duplicate = true
		} else {
			ctx = withActionLog(ctx, in.RequestID, "command", "", "修改资料使用授权")
			if err := beginActionLogTx(ctx, tx); err != nil {
				return err
			}
			out, err = s.mutateSourceAuthorizationTx(ctx, tx, scope, in)
			if err != nil {
				return err
			}
			if err := finishSourceCommandTx(ctx, tx, scope, in.RequestID, hash[:], revision); err != nil {
				return err
			}
		}
		out.ActionID = in.RequestID
		out.Undoable, err = sourceActionUndoableTx(ctx, tx, scope, in.RequestID)
		return err
	})
	return out, err
}

func (s *Store) SourceScope(ctx context.Context, scope memory.Scope, id memory.ID) (memory.SourceScopeResult, error) {
	var out memory.SourceScopeResult
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, err = readSourceScopeTx(ctx, tx, scope, id)
		return err
	})
	return out, err
}

func (s *Store) SetSourceScope(ctx context.Context, scope memory.Scope, in memory.SourceScopeRequest) (memory.SourceScopeResult, error) {
	var out memory.SourceScopeResult
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(in.RequestID).Valid() || !validSourceRef(in.Source) || in.ExpectedRevision < 0 {
		return out, memory.ErrInvalid
	}
	hash := sha256.Sum256(asJSON(in))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		revision, duplicate, err := s.beginSourceCommandTx(ctx, tx, scope, in.RequestID, hash[:])
		if err != nil {
			return err
		}
		if duplicate {
			if _, err := lockSourceMutationTx(ctx, tx, scope.OwnerID, in.Source); err != nil {
				return err
			}
			out, err = readSourceScopeTx(ctx, tx, scope, in.Source.ID)
			if err != nil {
				return err
			}
			if out.Revision != in.ExpectedRevision+1 {
				return memory.ErrConflict
			}
			out.Duplicate = true
		} else {
			ctx = withActionLog(ctx, in.RequestID, "command", "", "修改资料工作室范围")
			if err := beginActionLogTx(ctx, tx); err != nil {
				return err
			}
			out, err = s.mutateSourceScopeTx(ctx, tx, scope, in)
			if err != nil {
				return err
			}
			if err := finishSourceCommandTx(ctx, tx, scope, in.RequestID, hash[:], revision); err != nil {
				return err
			}
		}
		out.ActionID = in.RequestID
		out.Undoable, err = sourceActionUndoableTx(ctx, tx, scope, in.RequestID)
		return err
	})
	return out, err
}
