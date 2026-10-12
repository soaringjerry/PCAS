package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// Plan and enqueue the targeted v3 sweep. Untouched non-rule v2 labels remain
// current; the ordinary organizer also continues handling new unorganized input.
func (s *Store) QueueRuleReorganize(ctx context.Context, scope memory.Scope) (int, error) {
	if !scope.IsOwner {
		return 0, memory.ErrForbidden
	}
	pending := 0
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT count(*)"+strings.ReplaceAll(organizeEligible, "AND cl.organize_after<=now()", "")+" AND cl.owner_id=$2 AND c.category='rule'", kernel.OrganizeVersion, string(scope.OwnerID)).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}
		_, err := enqueueOrganizeTx(ctx, tx, scope.OwnerID, time.Now(), kernel.OrganizeVersion)
		return err
	})
	return pending, err
}
