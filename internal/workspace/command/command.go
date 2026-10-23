// Package command applies one workspace command of the owner. It checks the
// command, sends it to the owner of what it changes, and keeps the rules of
// the commands that belong to no single item: a delegation, a quick capture,
// a document, the settings, an agent, a memory excluded from an item, a
// training sample, a failed job, and the deletion of an item. Storage gives
// it a Unit in the transaction of the command.
package command

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
	"github.com/soaringjerry/PCAS/internal/workspace/settings"
)

// defaultRunKind is the kind of the run of a delegation that names none.
const defaultRunKind = "draft"

// Unit is the storage of one command, in its transaction. Each method reads
// or writes; none decides.
type Unit interface {
	itemcommand.Effects
	itemcommand.CandidateWrites
	// Flows that their owners apply in storage.
	RestoreMemory(ctx context.Context, id string) error
	UndoEntityMerge(ctx context.Context, id string) error
	UndoAction(ctx context.Context, id string) error
	MemoryCommand(ctx context.Context, c workspace.Command) error
	RunCommand(ctx context.Context, c workspace.Command) error

	// Deadline is the memory and its version that a deadline comes from.
	Deadline(ctx context.Context, id string) (claim string, version int, err error)
	// ActiveMemory fails unless the memory is active.
	ActiveMemory(ctx context.Context, id string) error
	CompleteDeadline(ctx context.Context, claim string, version int, reason string) error

	Agent(ctx context.Context, id string) (workspace.Agent, error)
	Available(agent string) bool
	SaveAgent(ctx context.Context, agent workspace.Agent) error
	InitializeAgentMemories(ctx context.Context, agent string) error

	// Capture saves a quick note as a source and returns the source.
	Capture(ctx context.Context, request memory.IngestRequest) (memory.Ref, error)
	Candidate(ctx context.Context, id string) (workspace.Candidate, error)
	SaveCandidate(ctx context.Context, candidate workspace.Candidate) error

	// Origin says who makes a new item in this command.
	Origin(ctx context.Context) itemcommand.Origin
	// Actor is who changes a document.
	Actor(ctx context.Context) string
	ItemExists(ctx context.Context, id string) (bool, error)
	// SaveAction saves the item as an action of the owner's log.
	SaveAction(ctx context.Context, item workspace.Item, summary string) error
	// ItemUse says whether the item has items under it and a running run.
	ItemUse(ctx context.Context, id string) (children, running bool, err error)
	// DeleteItem removes the item with its reminders, documents, runs,
	// samples, dependencies, and the receipts of its changes.
	DeleteItem(ctx context.Context, item workspace.Item) error

	Document(ctx context.Context, id string) (workspace.Doc, error)
	DocumentExists(ctx context.Context, id string) (bool, error)
	DeleteDocument(ctx context.Context, id string) error
	SaveDoc(ctx context.Context, doc workspace.Doc) error

	Settings(ctx context.Context) (workspace.Settings, error)
	SaveSettings(ctx context.Context, settings workspace.Settings) error
	// RefreshTaskPlans plans each task again after a change of the workdays.
	RefreshTaskPlans(ctx context.Context) error

	// ToggleExclusion excludes the memory from the item's context, or
	// includes it again when it was excluded.
	ToggleExclusion(ctx context.Context, item, memory string) error
	// SetSampleState changes the state of a sample. A stale sample cannot be
	// included. changed is false when no sample changed.
	SetSampleState(ctx context.Context, id, state string) (changed bool, err error)
	// RetryJob queues a failed or blocked job again. changed is false when
	// no job changed.
	RetryJob(ctx context.Context, id string) (changed bool, err error)
	// AuthorizeRecovery links the next attempts of the job to its earlier
	// ones under the owner's request.
	AuthorizeRecovery(ctx context.Context, job, request string) error
}

// Apply applies one command. now is the execution time.
func Apply(ctx context.Context, u Unit, c workspace.Command, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339Nano)
	switch c.Type {
	case "completeDeadline":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		claim, version, err := u.Deadline(ctx, c.ID)
		if err != nil {
			return err
		}
		if c.MemoryID != "" && c.MemoryID != claim {
			return memory.ErrInvalid
		}
		if err := u.ActiveMemory(ctx, claim); err != nil {
			return err
		}
		return u.CompleteDeadline(ctx, claim, version, c.Reason)
	case "restoreMemory":
		return u.RestoreMemory(ctx, c.ID)
	case "undoEntityMerge":
		return u.UndoEntityMerge(ctx, c.ID)
	case "undoAction":
		return u.UndoAction(ctx, c.ID)
	case "delegateTask":
		return delegate(ctx, u, c, now)
	case "capture":
		if err := itemcommand.RequireText(c.Text); err != nil {
			return err
		}
		in, err := u.Capture(ctx, memory.IngestRequest{Connector: "capture", ExternalID: c.RequestID, ExternalVersion: "1", Title: "快速记录", Text: c.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		return u.SaveCandidate(ctx, workspace.Candidate{ID: string(memory.NewID()), Kind: "unknown", Text: c.Text, Confidence: 0, Source: workspace.SourceRef{SourceID: string(in.ID), Version: in.Version, Label: "快速记录", Excerpt: c.Text, At: stamp}, State: "pending", CreatedAt: stamp})
	case "acceptCandidate", "ignoreCandidate", "restoreCandidate", "mergeCandidate":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		v, err := u.Candidate(ctx, c.ID)
		if err != nil {
			return err
		}
		if err := itemcommand.ReviewCandidate(ctx, &v, c, u); err != nil {
			return err
		}
		return u.SaveCandidate(ctx, v)
	case "addTask", "addIdea", "addProject":
		item, err := itemcommand.New(ctx, c, u.Origin(ctx), u)
		if err != nil {
			return err
		}
		if item.ID, err = idOrNew(c.ID); err != nil {
			return err
		}
		exists, err := u.ItemExists(ctx, item.ID)
		if err != nil {
			return err
		}
		if exists {
			return memory.ErrConflict
		}
		return u.SaveAction(ctx, item, "创建")
	case "bulkStatus", "bulkDefer", "bulkMove", "bulkAccept", "bulkIgnore":
		commands, err := itemcommand.Expand(c, func(id string) (workspace.Candidate, error) { return u.Candidate(ctx, id) })
		if err != nil {
			return err
		}
		for _, next := range commands {
			if err := Apply(ctx, u, next, now); err != nil {
				return err
			}
		}
		return nil
	case "editMemory", "confirmMemory", "deleteMemory", "setMemoryVisibility", "pinMemory":
		return u.MemoryCommand(ctx, c)
	case "requestRun", "pasteRunResult", "adoptRun", "discardRun":
		return u.RunCommand(ctx, c)
	case "createDoc", "updateDoc", "deleteDoc":
		return document(ctx, u, c, stamp)
	case "updateSettings":
		if err := settings.CheckZone(c.Patch); err != nil {
			return err
		}
		current, err := u.Settings(ctx)
		if err != nil {
			return err
		}
		next, replan, err := settings.Apply(current, c.Patch)
		if err != nil {
			return err
		}
		if err := u.SaveSettings(ctx, next); err != nil {
			return err
		}
		if replan {
			return u.RefreshTaskPlans(ctx)
		}
		return nil
	case "updateAgent":
		current, err := u.Agent(ctx, c.ID)
		if err != nil {
			return err
		}
		agent, initialize, err := settings.ApplyAgent(current, c.Patch)
		if err != nil {
			return err
		}
		if initialize {
			if err := u.InitializeAgentMemories(ctx, agent.ID); err != nil {
				return err
			}
			agent.MemoryInitialized = true
		}
		return u.SaveAgent(ctx, agent)
	case "toggleContextMemory":
		if _, err := u.Item(ctx, c.ThingID); err != nil {
			return err
		}
		if !memory.ID(c.MemoryID).Valid() {
			return memory.ErrInvalid
		}
		if err := u.ActiveMemory(ctx, c.MemoryID); err != nil {
			return err
		}
		return u.ToggleExclusion(ctx, c.ThingID, c.MemoryID)
	case "setSampleState":
		if !memory.ID(c.ID).Valid() || !oneOf(c.State, "included", "excluded", "candidate") {
			return memory.ErrInvalid
		}
		changed, err := u.SetSampleState(ctx, c.ID, c.State)
		if err != nil {
			return err
		}
		if !changed {
			return memory.ErrConflict
		}
		return nil
	case "retryJob":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		changed, err := u.RetryJob(ctx, c.ID)
		if err != nil {
			return err
		}
		if !changed {
			return memory.ErrConflict
		}
		// Only this explicit owner command releases a terminal recovery pause.
		// Keep its old attempt records and link the next bounded chain to them.
		return u.AuthorizeRecovery(ctx, c.ID, c.RequestID)
	case "deleteThing":
		return deleteItem(ctx, u, c.ID)
	}
	// Remaining commands operate on a single action record.
	item, err := u.Item(ctx, itemcommand.Target(c))
	if err != nil {
		return err
	}
	summary, err := itemcommand.Apply(ctx, &item, c, u)
	if err != nil {
		return err
	}
	return u.SaveAction(ctx, item, summary)
}

// delegate makes a task and queues its run in one command. The request
// receipt of the command fences retries; a stable item ID also fences
// replays under a new request ID after a lost response or a reload.
func delegate(ctx context.Context, u Unit, c workspace.Command, now time.Time) error {
	if !memory.ID(c.ID).Valid() || itemcommand.RequireText(c.Prompt) != nil {
		return memory.ErrInvalid
	}
	agent, err := u.Agent(ctx, c.AgentID)
	if err != nil || !agent.Enabled || agent.Channel == "manual" || !u.Available(agent.ID) {
		return memory.ErrUnavailable
	}
	create := c
	create.Type = "addTask"
	create.Text = c.Prompt
	if err := Apply(ctx, u, create, now); err != nil {
		return err
	}
	run := c
	run.Type, run.ID, run.ThingID, run.Kind = "requestRun", "", c.ID, c.Kind
	if run.Kind == "" {
		run.Kind = defaultRunKind
	}
	return u.RunCommand(ctx, run)
}

// document creates, changes, or deletes a document. The owner creates a
// document with its own identity; a change keeps the version it was based on.
func document(ctx context.Context, u Unit, c workspace.Command, stamp string) error {
	var doc workspace.Doc
	if c.Type == "createDoc" {
		if c.Doc == nil {
			return memory.ErrInvalid
		}
		doc = *c.Doc
		doc.Version = 0
		doc.BasedOn = nil
		if !memory.ID(doc.ID).Valid() || doc.By != "user" || doc.RunID != "" {
			return memory.ErrInvalid
		}
		if _, err := u.Item(ctx, doc.ThingID); err != nil {
			return err
		}
		exists, err := u.DocumentExists(ctx, doc.ID)
		if err != nil {
			return err
		}
		if exists {
			return memory.ErrConflict
		}
		doc.CreatedAt = stamp
	} else {
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		var err error
		doc, err = u.Document(ctx, c.ID)
		if err != nil {
			return err
		}
		if c.Type == "deleteDoc" {
			return u.DeleteDocument(ctx, c.ID)
		}
		base := doc.Version
		doc.BasedOn = &base
		doc.By = u.Actor(ctx)
		if err := itemcommand.Patch(&doc, c.Patch, "title", "body"); err != nil {
			return err
		}
	}
	if err := itemcommand.RequireText(doc.Title); err != nil {
		return err
	}
	doc.UpdatedAt = stamp
	return u.SaveDoc(ctx, doc)
}

// deleteItem removes a task, an idea, or an empty project for good. A
// project with items under it, and an item whose run is working, stay. The
// deletion is confirmed in the interface and cannot be undone.
func deleteItem(ctx context.Context, u Unit, id string) error {
	item, err := u.Item(ctx, id)
	if err != nil {
		return err
	}
	children, running, err := u.ItemUse(ctx, item.ID)
	if err != nil {
		return err
	}
	if children {
		return memory.ErrConflict
	}
	if running {
		return workspace.ErrWorkStarted
	}
	return u.DeleteItem(ctx, item)
}

func idOrNew(id string) (string, error) {
	if id == "" {
		return string(memory.NewID()), nil
	}
	if !memory.ID(id).Valid() {
		return "", memory.ErrInvalid
	}
	return id, nil
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
