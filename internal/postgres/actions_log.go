package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type actionLogKey struct{}
type actionLog struct{ id, source, turnID, summary string }
type actionChange struct {
	Table     string          `json:"table"`
	ID        string          `json:"id"`
	Before    json.RawMessage `json:"before"`
	AfterHash *string         `json:"afterHash"`
}

func withActionLog(ctx context.Context, id, source, turnID, summary string) context.Context {
	return context.WithValue(ctx, actionLogKey{}, actionLog{id, source, turnID, summary})
}
func beginActionLogTx(ctx context.Context, tx pgx.Tx) error {
	if ctx.Value(actionLogKey{}) == nil {
		return nil
	}
	_, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','[]',true)")
	return err
}
func flushActionLog(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	// Retain the audit metadata, including undone_at, after the undo window.
	// Run even when this transaction collected no new document changes.
	if _, err := tx.Exec(ctx, "UPDATE action_log SET changes='[]'::jsonb,expired_at=now() WHERE owner_id=$1 AND expired_at IS NULL AND created_at<now()-interval '30 days'", string(scope.OwnerID)); err != nil {
		return err
	}
	log, ok := ctx.Value(actionLogKey{}).(actionLog)
	if !ok {
		return nil
	}
	var changes []byte
	if err := tx.QueryRow(ctx, "SELECT current_setting('pcas.action_changes')::jsonb").Scan(&changes); err != nil {
		return err
	}
	var entries []actionChange
	if err := json.Unmarshal(changes, &entries); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','',true)"); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,turn_id,summary,changes) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), log.id, log.source, nullString(log.turnID), log.summary, changes)
	return err
}
func undoableCommand(t string) bool {
	return oneOf(t, "addTask", "addIdea", "addProject", "updateTask", "updateProject", "setTaskStatus", "renameThing", "setNotes", "moveThing", "deferTask", "addCheck", "toggleCheck", "removeCheck", "ideaPromote", "ideaSnooze", "ideaShelve", "ideaDrop", "ideaContinue", "addCondition", "removeCondition", "adoptRun", "discardRun", "createDoc", "updateDoc", "deleteDoc", "bulkStatus", "bulkDefer", "bulkMove")
}
func commandSummary(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) string {
	title := c.Title
	if title == "" {
		title = c.Name
	}
	id := c.ID
	if c.TaskID != "" {
		id = c.TaskID
	}
	if c.IdeaID != "" {
		id = c.IdeaID
	}
	if title == "" && memory.ID(id).Valid() {
		if item, err := getItem(ctx, tx, scope, id); err == nil {
			title = item.Title
		}
	}
	prefix := "修改："
	if oneOf(c.Type, "addTask", "addIdea", "addProject") {
		prefix = "新建："
	}
	if c.Type == "setTaskStatus" && c.Status == "done" {
		prefix = "完成："
	}
	return prefix + title
}
func (s *Store) undoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	ctx = withActor(ctx, "user")
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	var data []byte
	var summary string
	var undone, expired *string
	err := tx.QueryRow(ctx, "SELECT changes,summary,undone_at::text,expired_at::text FROM action_log WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), id).Scan(&data, &summary, &undone, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	if expired != nil {
		return workspace.ErrChangedSince
	}
	if undone != nil {
		return workspace.ErrAlreadyUndone
	}
	var changes []actionChange
	if err = json.Unmarshal(data, &changes); err != nil {
		return err
	}
	// Lock all rows first. The owner lock serializes commands and undo; run locks
	// fence the worker, which does not take the owner lock when claiming work.
	for _, c := range changes {
		if !oneOf(c.Table, "work_items", "work_documents", "agent_runs", "training_samples") {
			return memory.ErrInvalid
		}
		if c.Table == "agent_runs" && string(c.Before) == "null" {
			var status string
			e := tx.QueryRow(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&status)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && status != "queued" {
				return workspace.ErrWorkStarted
			}
		}
		if c.Table == "work_items" && string(c.Before) == "null" {
			rows, err := tx.Query(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 FOR UPDATE", string(scope.OwnerID), c.ID)
			if err != nil {
				return err
			}
			started := false
			for rows.Next() {
				var status string
				if err = rows.Scan(&status); err != nil {
					rows.Close()
					return err
				}
				if status != "queued" {
					started = true
				}
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if started {
				return workspace.ErrWorkStarted
			}
		}
		var hash string
		err = tx.QueryRow(ctx, "SELECT encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+c.Table+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&hash)
		if errors.Is(err, pgx.ErrNoRows) {
			if c.AfterHash != nil {
				return workspace.ErrChangedSince
			}
		} else if err != nil {
			return err
		} else if c.AfterHash == nil || hash != *c.AfterHash {
			return workspace.ErrChangedSince
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if string(c.Before) == "null" {
			if c.Table == "work_items" {
				var referenced bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2)", string(scope.OwnerID), c.ID).Scan(&referenced); err != nil {
					return err
				}
				if referenced {
					return workspace.ErrChangedSince
				}
				// Removing queued rows releases their reserved_cost from the daily sum.
				if _, err = tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), c.ID); err != nil {
					return err
				}
			}
			_, err = tx.Exec(ctx, "DELETE FROM "+c.Table+" WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		} else {
			switch c.Table {
			case "work_items":
				var item workspace.Item
				if err = json.Unmarshal(c.Before, &item); err != nil {
					return err
				}
				current, e := getItem(ctx, tx, scope, c.ID)
				if e != nil {
					return e
				}
				item.Version = current.Version + 1
				item.UpdatedAt = stamp()
				err = s.saveAction(ctx, tx, scope, item, "撤销："+summary)
			case "work_documents":
				var doc workspace.Doc
				if err = json.Unmarshal(c.Before, &doc); err == nil {
					err = saveDoc(ctx, tx, scope, doc)
				}
			case "agent_runs":
				var run workspace.Run
				if err = json.Unmarshal(c.Before, &run); err == nil {
					_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3,status=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID, c.Before, run.Status)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
	return err
}
func (s *Store) Undo(ctx context.Context, scope memory.Scope, actionID string) (workspace.State, error) {
	var out workspace.State
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.undoActionTx(ctx, tx, scope, actionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		var err error
		out, err = s.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
