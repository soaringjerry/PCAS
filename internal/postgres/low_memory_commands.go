package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func saveCandidate(ctx context.Context, tx pgx.Tx, scope memory.Scope, v workspace.Candidate) error {
	_, err := tx.Exec(ctx, `INSERT INTO capture_candidates(owner_id,id,source_id,source_version,state,document) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(owner_id,id) DO UPDATE SET state=excluded.state,document=excluded.document`, string(scope.OwnerID), v.ID, v.Source.SourceID, v.Source.Version, v.State, kernel.JSON(v))
	return err
}

func activeClaim(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_records WHERE owner_id=$1 AND id=$2 AND kind='claim' AND state='active')", string(scope.OwnerID), id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return memory.ErrNotFound
	}
	return nil
}

func sampleTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, sample workspace.Sample) error {
	_, err := tx.Exec(ctx, "INSERT INTO training_samples(owner_id,id,run_id,memory_id,state,stale,document) VALUES($1,$2,$3,$4,$5,$6,$7)", string(scope.OwnerID), sample.ID, kernel.NullString(sample.Origin.RunID), kernel.NullString(sample.Origin.MemoryID), sample.State, sample.Stale, kernel.JSON(sample))
	return err
}
