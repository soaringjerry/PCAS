package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/deputystore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ExecuteWith executes one workspace command. A command that queues a run
// searches memory first; embed gives that search the vector of its query.
func (s *Store) ExecuteWith(ctx context.Context, scope memory.Scope, in workspace.Command, embed retrieval.QueryEmbed) (workspace.State, error) {
	var out workspace.State
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(in.RequestID).Valid() || in.ExpectedRevision < 0 {
		return out, memory.ErrInvalid
	}
	hash := sha256.Sum256(kernel.JSON(in))
	if kernel.OneOf(in.Type, "requestRun", "delegateTask") {
		if kernel.RequireText(in.Prompt) != nil || in.Type == "delegateTask" && !memory.ID(in.ID).Valid() || in.Type == "requestRun" && !memory.ID(in.ThingID).Valid() {
			return out, memory.ErrInvalid
		}
		// Avoid a second embedding/budget reservation for a completed retry.
		var prior []byte
		err := s.Pool.QueryRow(ctx, "SELECT request_hash FROM workspace_commands WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), in.RequestID).Scan(&prior)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return out, memory.ErrConflict
			}
			return s.Snapshot(ctx, scope)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if in.Type == "delegateTask" {
			var exists bool
			if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), in.ID).Scan(&exists); err != nil {
				return out, err
			}
			if exists {
				return out, memory.ErrConflict
			}
		}
		var errPrepare error
		ctx, errPrepare = s.PrepareRunContext(ctx, scope, in, embed)
		if errPrepare != nil {
			return out, errPrepare
		}
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var revision int64
		if err := tx.QueryRow(ctx, "SELECT revision FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)).Scan(&revision); err != nil {
			return err
		}
		var prior []byte
		err := tx.QueryRow(ctx, "SELECT request_hash FROM workspace_commands WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), in.RequestID).Scan(&prior)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return memory.ErrConflict
			}
			out, err = s.SnapshotTx(ctx, tx, scope)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Only toggles and bulk operations depend on the observed workspace
		// state. Unrelated worker commits must not block other commands;
		// their request IDs and command-specific guards still apply.
		if (kernel.OneOf(in.Type, "toggleCheck", "toggleTrigger", "toggleContextMemory") || strings.HasPrefix(in.Type, "bulk")) && revision != in.ExpectedRevision {
			return memory.ErrConflict
		}
		recordAction := workspacestore.UndoableCommand(in.Type)
		if in.Type == "undoAction" {
			var err error
			recordAction, err = deputystore.ReviseUndoActionTx(ctx, tx, scope, in.ID)
			if err != nil {
				return err
			}
		}
		if recordAction {
			ctx = actionlog.WithActionLog(ctx, in.RequestID, "command", "", workspacestore.CommandSummary(ctx, tx, scope, in))
			if in.Type == "undoAction" {
				ctx = workspacestore.WithUndoOf(ctx, in.ID)
			}
			if err := actionlog.BeginActionLogTx(ctx, tx); err != nil {
				return err
			}
		}
		if err := s.commandTx(ctx, tx, scope, in); err != nil {
			return err
		}
		if err := actionlog.FlushActionLog(ctx, tx, scope); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO workspace_commands(owner_id,request_id,request_hash,revision) VALUES($1,$2,$3,$4)", string(scope.OwnerID), in.RequestID, hash[:], revision+1); err != nil {
			return err
		}
		out, err = s.SnapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
