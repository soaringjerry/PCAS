package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Only the server-owned action-log restore can replace a revoked tuple. This
// marker is never decoded from request JSON or accepted from model actions.
type sourcePolicyRestoreKey struct{}

func verifyContextPolicyUndoTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, change actionChange) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	if !memory.ID(change.ID).Valid() || change.AfterHash == nil || change.SourceVersion < 1 {
		return workspace.ErrChangedSince
	}
	expected, err := strconv.Atoi(*change.AfterHash)
	if err != nil || expected < 1 {
		return workspace.ErrChangedSince
	}
	var sourceID memory.ID
	var version, scopeRevision, revision int
	switch change.Table {
	case "source_authorizations":
		err = tx.QueryRow(ctx, `SELECT p.source_id::text,p.revision,r.version,coalesce(sr.revision,0) FROM source_authorizations p JOIN memory_records r ON (r.owner_id,r.id)=(p.owner_id,p.source_id) LEFT JOIN source_scope_revisions sr ON (sr.owner_id,sr.source_id)=(r.owner_id,r.id) WHERE p.owner_id=$1 AND p.id=$2 AND r.kind='source' AND r.state='active' FOR UPDATE OF p,r`, string(scope.OwnerID), change.ID).Scan(&sourceID, &revision, &version, &scopeRevision)
	case "source_scope_revisions":
		sourceID = memory.ID(change.ID)
		err = tx.QueryRow(ctx, `SELECT r.version,sr.revision FROM source_scope_revisions sr JOIN memory_records r ON (r.owner_id,r.id)=(sr.owner_id,sr.source_id) WHERE sr.owner_id=$1 AND sr.source_id=$2 AND r.kind='source' AND r.state='active' FOR UPDATE OF sr,r`, string(scope.OwnerID), change.ID).Scan(&version, &revision)
		scopeRevision = revision
	default:
		return memory.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return workspace.ErrChangedSince
	}
	if err != nil {
		return err
	}
	if !sourceID.Valid() || revision != expected || version != change.SourceVersion || scopeRevision != change.ScopeRevision {
		return workspace.ErrChangedSince
	}
	return nil
}

func (s *Store) undoContextPolicyChangeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, change actionChange) error {
	if err := verifyContextPolicyUndoTx(ctx, tx, scope, change); err != nil {
		return err
	}
	switch change.Table {
	case "source_authorizations":
		current, err := scanSourcePolicy(tx.QueryRow(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), change.ID))
		if err != nil {
			return err
		}
		var before *memory.SourceAuthorization
		if err := json.Unmarshal(change.Before, &before); err != nil {
			return memory.ErrInvalid
		}
		in := memory.SourceAuthorizationRequest{PolicyID: current.ID, Source: memory.Ref{ID: current.SourceID, Kind: memory.SourceKind, Version: change.SourceVersion}, ExpectedPolicyRevision: current.Revision, Recipient: current.Recipient, Purpose: current.Purpose, Scope: current.Scope}
		if before == nil {
			// Keep the row as a revision fence, with the permission meaning of
			// absence. It must not deny previously independent claim grants.
			in.Revoke = true
			ctx = context.WithValue(ctx, sourcePolicyUndoAbsenceKey{}, true)
		} else {
			if before.ID != current.ID || before.SourceID != current.SourceID || !before.Recipient.Valid() || !before.Scope.Valid() || !validSourcePurpose(before.Purpose) || before.Revision >= current.Revision {
				return memory.ErrInvalid
			}
			in.Recipient, in.Purpose, in.Scope, in.Revoke = before.Recipient, before.Purpose, before.Scope, before.Revoked
			ctx = context.WithValue(ctx, sourcePolicyUndoAbsenceKey{}, before.Revoked && !before.ExplicitDeny)
		}
		ctx = context.WithValue(ctx, sourcePolicyRestoreKey{}, true)
		_, err = s.mutateSourceAuthorizationTx(ctx, tx, scope, in)
		return err
	case "source_scope_revisions":
		var before memory.SourceScopeResult
		if err := json.Unmarshal(change.Before, &before); err != nil {
			return memory.ErrInvalid
		}
		if !validSourceRef(before.Source) || before.Source.ID != memory.ID(change.ID) || before.Source.Version != change.SourceVersion || before.Revision < 0 || before.Revision >= change.ScopeRevision {
			return memory.ErrInvalid
		}
		_, err := s.mutateSourceScopeTx(ctx, tx, scope, memory.SourceScopeRequest{Source: before.Source, ExpectedRevision: change.ScopeRevision, Assignments: before.Assignments})
		return err
	default:
		return memory.ErrInvalid
	}
}
