package effort

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Stage is the queue stage of an estimation job. The stage of one job is
// "memory.effort:<project id>:<job nonce>".
const Stage = "memory.effort"

// HourlyCalls is the number of effort estimates that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 10

// Snapshot is the input of one estimation. It is saved with the paid result,
// so that a recovered result is applied to the tasks that the model saw.
type Snapshot struct {
	Project    workspace.Item   `json:"project"`
	Items      []workspace.Item `json:"items"`
	DailyHours float64          `json:"dailyHours"`
	At         time.Time        `json:"at"`
}

// Store reads and writes the workspace records of one estimation job.
type Store interface {
	// Load reads the project under the job's lease. When wantTasks is set it
	// also reads the project's tasks that have a due time and no estimate, and
	// that the user has not estimated. Load acknowledges the job and reports
	// done when the project is gone, or when wantTasks is set and no task needs
	// an estimate.
	Load(ctx context.Context, job worker.Job, project string, wantTasks bool) (item workspace.Item, tasks []workspace.Item, done bool, err error)
	// Apply runs fn in one transaction that holds the owner and the job's lease.
	Apply(ctx context.Context, job worker.Job, fn func(Unit) error) error
}

// Unit is the set of writes that one Apply transaction permits.
type Unit interface {
	Location() (*time.Location, error)
	// Task returns the current task. found is false when it no longer exists.
	Task(id string) (task workspace.Item, found bool, err error)
	// SaveTask stores a task through the workspace item rules.
	SaveTask(workspace.Item) error
	// Event records a visible stage outcome with its reason.
	Event(outcome, reason string, count int) error
	DiscardResult() error
	// Requeue returns the job to the queue with the queue's own delay.
	Requeue(code string) error
	Acknowledge() error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	// GenerateForJob returns the saved input record and the model output. A
	// saved result is returned without another call. modelInput is the text
	// that the provider receives. record is stored with the result.
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

// Service estimates the effort of a project's tasks.
type Service struct {
	Store     Store
	Generator Generator
	// Now supplies business time for the model input.
	Now func() time.Time
}

type estimate struct {
	ItemID string   `json:"itemId"`
	Hours  *float64 `json:"hours"`
	Reason string   `json:"reason"`
}

// Process handles one estimation job.
// The model proposes hours. This service applies a proposal only to a task
// that is unchanged since the snapshot and that has no estimate yet.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	parts := strings.Split(job.Stage, ":")
	if len(parts) != 3 || parts[0] != Stage || !memory.ID(parts[1]).Valid() {
		return memory.ErrInvalid
	}
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	snapshot := Snapshot{DailyHours: DailyHours, At: s.Now().UTC(), Items: []workspace.Item{}}
	var done bool
	var tasks []workspace.Item
	snapshot.Project, tasks, done, err = s.Store.Load(ctx, job, parts[1], !saved)
	if err != nil || done {
		return err
	}
	if tasks != nil {
		snapshot.Items = tasks
	}
	record, output, err := s.Generator.GenerateForJob(ctx, job, "effort", prompts.Must("effort"), modelInput(snapshot), asJSON(map[string]any{"rawPrompt": modelInput(snapshot), "snapshot": snapshot}), nil)
	if err != nil {
		return err
	}
	// A recovered result carries the snapshot that its call used.
	var receipt struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err = json.Unmarshal(record, &receipt); err != nil {
		return err
	}
	snapshot = receipt.Snapshot
	var proposal struct {
		Items []estimate `json:"items"`
	}
	valid := json.Unmarshal([]byte(output), &proposal) == nil && proposal.Items != nil
	if !valid {
		proposal.Items = nil
	}
	return s.Store.Apply(ctx, job, func(u Unit) error { return apply(u, snapshot, proposal.Items, valid) })
}

func modelInput(snapshot Snapshot) string {
	items := []map[string]any{}
	for _, item := range snapshot.Items {
		items = append(items, map[string]any{"id": item.ID, "title": item.Title, "notes": item.Notes, "body": item.Body, "due": item.Due, "status": item.Status, "checklist": item.Checklist, "dependsOn": item.DependsOn})
	}
	return string(asJSON(map[string]any{"project": map[string]any{"id": snapshot.Project.ID, "title": snapshot.Project.Title, "goal": snapshot.Project.Goal, "status": snapshot.Project.Status}, "items": items, "dailyHours": snapshot.DailyHours, "at": snapshot.At}))
}

func apply(u Unit, snapshot Snapshot, proposals []estimate, valid bool) error {
	originals := map[string]workspace.Item{}
	for _, task := range snapshot.Items {
		originals[task.ID] = task
	}
	loc, err := u.Location()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	invalid := 0
	for _, p := range proposals {
		before, ok := originals[p.ItemID]
		if !ok || seen[p.ItemID] || p.Hours == nil || strings.TrimSpace(p.Reason) == "" {
			invalid++
			continue
		}
		if _, err := StartDate(before.Due, p.Hours, loc); err != nil {
			invalid++
			continue
		}
		seen[p.ItemID] = true
		current, found, err := u.Task(p.ItemID)
		if err != nil {
			return err
		}
		// Only this task's unchanged input can be updated. A user estimate or a
		// concurrent edit is never overwritten by this older paid snapshot.
		if !found || current.ProjectID != snapshot.Project.ID || current.EstimatedHours != nil || current.EffortSource == "user" || workspace.ContentHash(current) != workspace.ContentHash(before) {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		current.EstimatedHours = p.Hours
		current.EffortSource = "model"
		current.EffortReason = p.Reason
		current.Version++
		current.UpdatedAt = now
		current.History = append(current.History, workspace.Revision{At: now, By: "system", Summary: "估计工作量：" + FormatHours(*p.Hours) + "小时；" + p.Reason})
		if err := u.SaveTask(current); err != nil {
			return err
		}
	}
	missing := 0
	unresolved := false
	for _, task := range snapshot.Items {
		if seen[task.ID] {
			continue
		}
		missing++
		current, found, err := u.Task(task.ID)
		if err != nil {
			return err
		}
		if found && current.ProjectID == snapshot.Project.ID && current.EstimatedHours == nil && current.EffortSource != "user" && current.Due != "" {
			unresolved = true
		}
	}
	if missing > 0 {
		if err := u.Event("overflow", "effort_missing_items", missing); err != nil {
			return err
		}
	}
	if invalid > 0 {
		if err := u.Event("overflow", "effort_invalid_items", invalid); err != nil {
			return err
		}
	}
	if err := u.DiscardResult(); err != nil {
		return err
	}
	if !valid || unresolved {
		// A task still needs an estimate. Report it and let the queue try again.
		if err := u.Event("failure", "effort_incomplete_output", 1); err != nil {
			return err
		}
		return u.Requeue("effort_incomplete_output")
	}
	return u.Acknowledge()
}

func asJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
