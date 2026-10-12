package kernel

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// StageEvent records one outcome of a background stage in the transaction.
// A failure also marks the stage as failed for the write that hands the
// job's slot back.
func StageEvent(ctx context.Context, tx pgx.Tx, owner memory.ID, stage, outcome, reason string, count int) error {
	if outcome == "failure" {
		// The job's slot is handed back in this same write; that is not a success.
		if _, err := tx.Exec(ctx, "SELECT set_config('pcas.stage_failed',$1,true)", stage); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, "INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,$2,$3,$4,$5)", NullString(string(owner)), stage, outcome, reason, count)
	return err
}

// DiscardPaidResult removes the saved paid result of a job.
func DiscardPaidResult(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	_, err := tx.Exec(ctx, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2", string(j.OwnerID), string(j.ID))
	return err
}
