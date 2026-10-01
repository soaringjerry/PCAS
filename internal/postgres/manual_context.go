package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ManualRunPackage reassembles the selected run for its original, explicitly
// configured destination. A new destination requires a new owner requestRun.
// No prepared raw body is copied into the durable run document.
func (s *Store) ManualRunPackage(ctx context.Context, scope memory.Scope, id string) (map[string]any, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	if !memory.ID(id).Valid() {
		return nil, memory.ErrInvalid
	}
	var run workspace.Run
	var entries []memory.EvidenceEntry
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		run, err = queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
		if err != nil {
			return err
		}
		if run.Status != "waiting" || run.StaleContext || run.ContextTask == nil || run.ManualRecipient == nil || run.ContextTask.Recipient.Role != "manual" || run.ContextTask.Recipient.Channel != "manual" {
			return memory.ErrConflict
		}
		entries, err = s.hydrateRunInputTx(ctx, tx, scope, run)
		return err
	})
	if err != nil {
		return nil, err
	}
	body := assistantInstructions + "\n\n" + renderRunInput(run.Brief, entries)
	candidates := run.ContextCandidates
	attempt, err := s.prepareContextAttempt(ctx, scope, run.ID, *run.ContextTask, run.ContextDependencies, entries, candidates, indirectRunDependencies(run, entries), []byte(body), "manual_package")
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		current, err := queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), id)
		if err != nil {
			return err
		}
		if current.Status != "waiting" || current.ContextTask == nil || current.ManualRecipient == nil {
			return memory.ErrConflict
		}
		if err := s.verifyRunTx(ctx, tx, scope, current); err != nil {
			return err
		}
		if err := verifyContextAttemptTx(ctx, tx, scope, attempt.ID, *current.ContextTask, current.ContextDependencies); err != nil {
			return err
		}
		if err := persistContextArtifactDependenciesTx(ctx, tx, scope, "manual_package", id, 1, *current.ContextTask, current.ContextDependencies); err != nil {
			return err
		}
		current.ContextAttemptID = attempt.ID
		_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, asJSON(current))
		return err
	})
	if err != nil {
		return nil, err
	}
	// This is PCAS delivering a package, never a claim that an external model
	// received it. The helper rechecks dependencies inside its short transaction.
	if err = s.markContextAttemptDelivered(ctx, scope, attempt.ID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	attempt.DeliveredAt = &now
	return map[string]any{"run_id": id, "package": body, "attempt": attempt}, nil
}
