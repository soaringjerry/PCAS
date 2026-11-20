package commandstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/command"
)

// UndoActionTx reverses one action in the transaction tx. command.Undo
// decides; undoUnit reads and writes.
func (s *Commands) UndoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	return command.Undo(ctx, undoUnit{store: s, tx: tx, scope: scope}, id, time.Now())
}

// undoUnit is the storage of one undo.
type undoUnit struct {
	store *Commands
	tx    pgx.Tx
	scope memory.Scope
}

func (u undoUnit) owner() string { return string(u.scope.OwnerID) }

func (u undoUnit) TopicProject(ctx context.Context, id string) (bool, error) {
	return u.store.UndoTopicProjectTx(ctx, u.tx, u.scope, id)
}

func (u undoUnit) Comparison(ctx context.Context, id string) (bool, error) {
	return undoComparisonActionTx(ctx, u.tx, u.scope, id)
}

func (u undoUnit) Action(ctx context.Context, id string) (context.Context, command.Action, error) {
	ctx = kernel.WithActor(ctx, "user")
	action := command.Action{ID: id}
	var data []byte
	var undone *string
	err := u.tx.QueryRow(ctx, "SELECT changes,summary,undone_at::text,expired_at IS NOT NULL OR created_at<now()-interval '30 days',created_at,action_order,source,turn_id::text FROM action_log WHERE owner_id=$1 AND id=$2 FOR UPDATE", u.owner(), id).Scan(&data, &action.Summary, &undone, &action.Expired, &action.Created, &action.Order, &action.Source, &action.Turn)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, action, memory.ErrNotFound
	}
	if err != nil {
		return ctx, action, err
	}
	action.Undone = undone != nil
	var check string
	if e := u.tx.QueryRow(ctx, "SELECT smoke_id::text FROM desk_smoke_actions WHERE owner_id=$1 AND action_id=$2", u.owner(), id).Scan(&check); e == nil {
		ctx = actionlog.WithSmoke(ctx, check)
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return ctx, action, e
	}
	if err := json.Unmarshal(data, &action.Changes); err != nil {
		return ctx, action, err
	}
	return ctx, action, nil
}

func (u undoUnit) CheckSuccessors(ctx context.Context, action command.Action) error {
	return workspacestore.CheckActionSuccessors(ctx, u.tx, u.scope, action.ID, kernel.JSON(action.Changes), action.Created, action.Order, action.Source, action.Turn)
}

func changeOf(c command.Change) actionlog.ActionChange {
	return actionlog.ActionChange{Table: c.Table, ID: c.ID, Before: c.Before, AfterHash: c.AfterHash}
}

func (u undoUnit) CheckDeadlineCompletion(ctx context.Context, c command.Change) error {
	return workstore.CheckDeadlineCompletionTx(ctx, u.tx, u.scope, changeOf(c))
}

func (u undoUnit) RestoreDeadlineCompletion(ctx context.Context, c command.Change) error {
	return workstore.RestoreDeadlineCompletionTx(ctx, u.tx, u.scope, changeOf(c))
}

func (u undoUnit) AttachmentCurrent(ctx context.Context, ref memory.Ref) (bool, error) {
	var active bool
	err := u.tx.QueryRow(ctx, "SELECT r.state='active' AND r.version=$3 AND v.blob_key IS NOT NULL FROM memory_records r JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1 AND r.id=$2 FOR UPDATE OF r", u.owner(), string(ref.ID), ref.Version).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return active, err
}

func (u undoUnit) DeleteAttachment(ctx context.Context, ref memory.Ref) error {
	return u.store.DeleteTx(context.WithValue(ctx, intakestore.RetainUndoneAnswersKey{}, true), u.tx, u.scope, memory.DeleteRequest{Targets: []memory.Ref{ref}})
}

func (u undoUnit) RunStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := u.tx.QueryRow(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", u.owner(), id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return status, err
}

func (u undoUnit) RunStatuses(ctx context.Context, item string) ([]string, error) {
	rows, err := u.tx.Query(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 FOR UPDATE", u.owner(), item)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := []string{}
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}

// Unchanged compares the trigger's content fingerprint; an action collected
// before migration 019, whose after snapshot is absent, matches the whole
// document.
func (u undoUnit) Unchanged(ctx context.Context, c command.Change) (bool, error) {
	var hash, legacyHash string
	err := u.tx.QueryRow(ctx, "SELECT action_document_hash(document),encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+c.Table+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", u.owner(), c.ID).Scan(&hash, &legacyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.AfterHash == nil, nil
	}
	if err != nil {
		return false, err
	}
	return c.AfterHash != nil && (hash == *c.AfterHash || legacyHash == *c.AfterHash), nil
}

func (u undoUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u undoUnit) Children(ctx context.Context, project string) ([]workspace.Item, error) {
	return kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM work_items WHERE owner_id=$1 AND project_id=$2 ORDER BY id FOR UPDATE", u.owner(), project)
}

func (u undoUnit) SaveAction(ctx context.Context, item workspace.Item, summary string) error {
	return u.store.SaveAction(ctx, u.tx, u.scope, item, summary)
}

func (u undoUnit) RemoveItem(ctx context.Context, id string) error {
	// Removing queued rows releases their reserved_cost from the daily sum.
	if _, err := u.tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", u.owner(), id); err != nil {
		return err
	}
	return itemstore.DeleteItem(ctx, u.tx, u.scope.OwnerID, id)
}

func (u undoUnit) RemoveRow(ctx context.Context, c command.Change) error {
	_, err := u.tx.Exec(ctx, "DELETE FROM "+c.Table+" WHERE owner_id=$1 AND id=$2", u.owner(), c.ID)
	return err
}

func (u undoUnit) Suppress(ctx context.Context, item workspace.Item, trigger workspace.Trigger) error {
	return workspacestore.SuppressNoticeTx(ctx, u.tx, u.scope, item, trigger)
}

func (u undoUnit) RestoreDocument(ctx context.Context, doc workspace.Doc) error {
	return workspacestore.RestoreDocumentTx(ctx, u.tx, u.scope, doc)
}

func (u undoUnit) RestoreRun(ctx context.Context, c command.Change, status string) error {
	_, err := u.tx.Exec(ctx, "UPDATE agent_runs SET document=$3,status=$4 WHERE owner_id=$1 AND id=$2", u.owner(), c.ID, c.Before, status)
	return err
}

func (u undoUnit) Undone(ctx context.Context, action command.Action) error {
	if _, err := u.tx.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", u.owner(), action.ID); err != nil {
		return err
	}
	changes := make([]actionlog.ActionChange, 0, len(action.Changes))
	for _, c := range action.Changes {
		changes = append(changes, changeOf(c))
	}
	if err := intakestore.RefreshSmokeChangesTx(ctx, u.tx, u.scope, changes); err != nil {
		return err
	}
	return u.store.DeleteUndoneAdoptionSourcesTx(ctx, u.tx, u.scope, action.ID)
}

func (u undoUnit) ForgetTurn(ctx context.Context, turn string) error {
	return u.store.DeleteUndoneTurnMemoriesTx(ctx, u.tx, u.scope, turn)
}

func (s *Commands) Undo(ctx context.Context, scope memory.Scope, actionID string) (workspace.State, error) {
	var out workspace.State
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.UndoActionTx(ctx, tx, scope, actionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		var err error
		out, err = s.SnapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
