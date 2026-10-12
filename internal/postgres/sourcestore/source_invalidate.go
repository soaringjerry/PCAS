package sourcestore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/memorystore"
)

func InvalidateTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	for _, sql := range []string{
		`WITH RECURSIVE affected(id) AS (SELECT $2::uuid UNION SELECT d.view_id FROM derived_dependencies d JOIN affected a ON a.id=d.dependency_id WHERE d.owner_id=$1) UPDATE derived_views SET stale=true WHERE owner_id=$1 AND id IN (SELECT id FROM affected)`,
		`UPDATE agent_runs SET document=jsonb_set(document,'{staleContext}','true') WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=$2)`,
		`UPDATE training_samples SET stale=true WHERE owner_id=$1 AND (memory_id=$2 OR run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=$2))`,
		`DELETE FROM embeddings WHERE owner_id=$1 AND record_id=$2`,
	} {
		if _, err := tx.Exec(ctx, sql, string(scope.OwnerID), id); err != nil {
			return err
		}
	}
	return memorystore.RefreshSummaryJobsTx(ctx, tx, scope.OwnerID, memory.ID(id))
}
