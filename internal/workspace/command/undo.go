package command

import (
	"context"
	"encoding/json"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/reminder"
)

// Change is one row that an action changed: its table, its identity, its
// content before the action (null when the action made it), and the hash
// of its content after the action.
type Change struct {
	Table     string          `json:"table"`
	ID        string          `json:"id"`
	Before    json.RawMessage `json:"before"`
	AfterHash *string         `json:"afterHash"`
}

func (c Change) made() bool { return string(c.Before) == "null" }

// Action is one entry of the owner's action log.
type Action struct {
	ID, Summary, Source string
	Changes             []Change
	Undone, Expired     bool
	Created             time.Time
	Order               *int64
	// Turn is the secretary turn of the action, or nil.
	Turn *string
}

// UndoUnit is the storage of one undo, in the transaction of the command.
// Each method reads or writes; none decides.
type UndoUnit interface {
	// TopicProject and Comparison undo the actions that their owners keep;
	// handled is false for another action.
	TopicProject(ctx context.Context, id string) (handled bool, err error)
	Comparison(ctx context.Context, id string) (handled bool, err error)
	// Action reads and locks the entry, and gives ctx the smoke check of the
	// action when it has one.
	Action(ctx context.Context, id string) (context.Context, Action, error)
	// CheckSuccessors fails when a later action changed the same rows.
	CheckSuccessors(ctx context.Context, action Action) error
	CheckDeadlineCompletion(ctx context.Context, change Change) error
	RestoreDeadlineCompletion(ctx context.Context, change Change) error
	// AttachmentCurrent is true when the attached source is still the version
	// the action attached, with its file.
	AttachmentCurrent(ctx context.Context, source memory.Ref) (bool, error)
	DeleteAttachment(ctx context.Context, source memory.Ref) error
	// RunStatus is the status of a run, or empty when it is gone.
	RunStatus(ctx context.Context, id string) (string, error)
	// RunStatuses are the statuses of the runs of an item.
	RunStatuses(ctx context.Context, item string) ([]string, error)
	// Unchanged is true when the row is as the action left it.
	Unchanged(ctx context.Context, change Change) (bool, error)
	Item(ctx context.Context, id string) (workspace.Item, error)
	// Children are the items of a project.
	Children(ctx context.Context, project string) ([]workspace.Item, error)
	SaveAction(ctx context.Context, item workspace.Item, summary string) error
	// RemoveItem removes an item that the action made, with its runs.
	RemoveItem(ctx context.Context, id string) error
	// RemoveRow removes another row that the action made.
	RemoveRow(ctx context.Context, change Change) error
	// Suppress keeps a passed reminder of a reopened task from being sent.
	Suppress(ctx context.Context, item workspace.Item, trigger workspace.Trigger) error
	RestoreDocument(ctx context.Context, doc workspace.Doc) error
	RestoreRun(ctx context.Context, change Change, status string) error
	// Undone marks the action undone and removes what its adoptions remembered.
	Undone(ctx context.Context, action Action) error
	// ForgetTurn removes what an undone secretary turn remembered on its own.
	ForgetTurn(ctx context.Context, turn string) error
}

// undoTables are the tables whose rows an undo restores.
var undoTables = map[string]bool{"work_items": true, "work_documents": true, "agent_runs": true, "training_samples": true}

// Undo reverses one action of the owner. An action is undone once, within
// thirty days, while no later action changed its rows and no run it queued
// has started. A project that the secretary made can be undone with items in
// it; they go back to no project. now is the execution time.
func Undo(ctx context.Context, u UndoUnit, id string, now time.Time) error {
	if handled, err := u.TopicProject(ctx, id); handled {
		return err
	}
	if handled, err := u.Comparison(ctx, id); handled {
		return err
	}
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	ctx, action, err := u.Action(ctx, id)
	if err != nil {
		return err
	}
	if action.Undone {
		return workspace.ErrAlreadyUndone
	}
	if action.Expired {
		return workspace.ErrExpired
	}
	// Recorded successors take precedence over external fingerprint changes
	// and work-start checks.
	if err := u.CheckSuccessors(ctx, action); err != nil {
		return err
	}
	for _, c := range action.Changes {
		if err := undoable(ctx, u, c); err != nil {
			return err
		}
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	for i := len(action.Changes) - 1; i >= 0; i-- {
		if err := restore(ctx, u, action, action.Changes[i], stamp, now); err != nil {
			return err
		}
	}
	if err := u.Undone(ctx, action); err != nil {
		return err
	}
	if action.Source != "desk" || action.Turn == nil {
		return nil
	}
	return u.ForgetTurn(ctx, *action.Turn)
}

// undoable checks that one change can be reversed.
func undoable(ctx context.Context, u UndoUnit, c Change) error {
	switch {
	case c.Table == "deadline_completion":
		return u.CheckDeadlineCompletion(ctx, c)
	case c.Table == "source_attachments":
		var ref memory.Ref
		if json.Unmarshal(c.Before, &ref) != nil || ref.Kind != memory.SourceKind || string(ref.ID) != c.ID || ref.Version < 1 {
			return memory.ErrInvalid
		}
		current, err := u.AttachmentCurrent(ctx, ref)
		if err != nil {
			return err
		}
		if !current {
			return workspace.ErrExpired
		}
		return nil
	case !undoTables[c.Table]:
		return memory.ErrInvalid
	}
	if c.Table == "agent_runs" && c.made() {
		status, err := u.RunStatus(ctx, c.ID)
		if err != nil {
			return err
		}
		if status != "" && status != "queued" {
			return workspace.ErrWorkStarted
		}
	}
	if c.Table == "work_items" && c.made() {
		statuses, err := u.RunStatuses(ctx, c.ID)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if status != "queued" {
				return workspace.ErrWorkStarted
			}
		}
	}
	unchanged, err := u.Unchanged(ctx, c)
	if err != nil {
		return err
	}
	if !unchanged {
		return workspace.ErrChangedSince
	}
	return nil
}

// restore reverses one change.
func restore(ctx context.Context, u UndoUnit, action Action, c Change, stamp string, now time.Time) error {
	switch {
	case c.Table == "deadline_completion":
		return u.RestoreDeadlineCompletion(ctx, c)
	case c.Table == "source_attachments":
		var ref memory.Ref
		if err := json.Unmarshal(c.Before, &ref); err != nil {
			return err
		}
		return u.DeleteAttachment(ctx, ref)
	case c.made() && c.Table == "work_items":
		children, err := u.Children(ctx, c.ID)
		if err != nil {
			return err
		}
		if len(children) > 0 {
			project, err := u.Item(ctx, c.ID)
			if err != nil {
				return err
			}
			if project.Kind != "project" || project.Creation == nil || project.Creation.By != "secretary" {
				return workspace.ErrChangedSince
			}
			for _, item := range children {
				item.ProjectID = ""
				item.Version++
				item.UpdatedAt = stamp
				if err := u.SaveAction(ctx, item, "撤销项目，事项回到无项目"); err != nil {
					return err
				}
			}
		}
		return u.RemoveItem(ctx, c.ID)
	case c.made():
		return u.RemoveRow(ctx, c)
	}
	switch c.Table {
	case "work_items":
		var item workspace.Item
		if err := json.Unmarshal(c.Before, &item); err != nil {
			return err
		}
		current, err := u.Item(ctx, c.ID)
		if err != nil {
			return err
		}
		for _, trigger := range reminder.Suppressed(current, item, now) {
			if err := u.Suppress(ctx, item, trigger); err != nil {
				return err
			}
		}
		item.Version = current.Version + 1
		item.UpdatedAt = stamp
		return u.SaveAction(ctx, item, "撤销："+action.Summary)
	case "work_documents":
		var doc workspace.Doc
		if err := json.Unmarshal(c.Before, &doc); err != nil {
			return err
		}
		return u.RestoreDocument(ctx, doc)
	case "agent_runs":
		var run workspace.Run
		if err := json.Unmarshal(c.Before, &run); err != nil {
			return err
		}
		return u.RestoreRun(ctx, c, run.Status)
	}
	return nil
}
