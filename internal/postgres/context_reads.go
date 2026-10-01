package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Owner audit remains available without a model task. A coarse source grant
// alone never authorizes raw material to an unspecified external recipient.
func contextReadAllowsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) (bool, error) {
	if scope.Task == nil {
		if scope.IsOwner {
			return true, nil
		}
		return ref.Kind != memory.SourceKind && ref.Kind != memory.SummaryKind && ref.Kind != memory.ChunkKind, nil
	}
	if scope.Task.OwnerID != scope.OwnerID || (!scope.IsOwner && scope.Task.Recipient.PrincipalID != scope.PrincipalID) {
		return false, memory.ErrForbidden
	}
	_, err := hydrateTypedOneTx(ctx, tx, scope, *scope.Task, ref, map[memory.Ref]bool{}, 0)
	if errors.Is(err, memory.ErrForbidden) || errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrNotFound) || errors.Is(err, memory.ErrInvalid) || errors.Is(err, memory.ErrUnavailable) {
		return false, nil
	}
	return err == nil, err
}

func contextEvidenceAllowsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) (bool, error) {
	if scope.Task == nil {
		return scope.IsOwner, nil
	}
	return contextReadAllowsTx(ctx, tx, scope, ref)
}
