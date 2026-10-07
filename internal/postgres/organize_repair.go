package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"strings"
	"time"
)

// Plan and enqueue the targeted v3 sweep. Untouched non-rule v2 labels remain
// current; the ordinary organizer also continues handling new unorganized input.
func (s *Store) QueueRuleReorganize(ctx context.Context, scope memory.Scope) (int, error) {
	if !scope.IsOwner {
		return 0, memory.ErrForbidden
	}
	pending := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := extractionOwnerLock(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT count(*)"+strings.ReplaceAll(organizeEligible, "AND cl.organize_after<=now()", "")+" AND cl.owner_id=$2 AND c.category='rule'", OrganizeVersion, string(scope.OwnerID)).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}
		_, err := enqueueOrganizeTx(ctx, tx, scope.OwnerID, time.Now(), OrganizeVersion)
		return err
	})
	return pending, err
}
