package deputy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	// subtaskLimit is the count of subtasks that one adoption makes.
	subtaskLimit = 100
	// subtaskLength is the longest subtask, in bytes.
	subtaskLength = 2000
)

// CheckAdoption checks that the result of a run can be adopted in the given
// way. Only a finished run whose context is still current is adopted, and
// only once. A revision is adopted as a document.
func CheckAdoption(run workspace.Run, as, text string) error {
	if run.Status != "done" || run.Adopted != nil || run.StaleContext {
		return memory.ErrConflict
	}
	if strings.TrimSpace(text) == "" || len(text) > 1<<20 {
		return memory.ErrInvalid
	}
	switch as {
	case "doc", "subtasks", "progress":
	default:
		return memory.ErrInvalid
	}
	if run.Kind == "revise" && as != "doc" {
		return memory.ErrInvalid
	}
	return nil
}

// Subtasks returns the subtasks that a text becomes. A text gives one
// subtask at least and one hundred at most.
func Subtasks(text string) ([]string, error) {
	out := []string{}
	for _, line := range AdoptionLines(text) {
		if line == "" {
			continue
		}
		if len(line) > subtaskLength || len(out) == subtaskLimit {
			return nil, memory.ErrInvalid
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, memory.ErrInvalid
	}
	return out, nil
}

// AdoptUnit is the storage of one adoption, in the transaction of the
// command or of the finished run. Each method reads or writes; none decides.
type AdoptUnit interface {
	Agent(ctx context.Context, id string) (workspace.Agent, error)
	// VerifyAccess checks that the agent may still use what the run used.
	VerifyAccess(ctx context.Context, run workspace.Run) error
	Item(ctx context.Context, id string) (workspace.Item, error)
	// ClearAdoption removes the provenance rows of an earlier adoption of the
	// run, which an undo leaves in place.
	ClearAdoption(ctx context.Context, run string) error
	// Revise writes the text of a revision as the next version of its
	// document.
	Revise(ctx context.Context, run workspace.Run, text string) error
	SaveDoc(ctx context.Context, doc workspace.Doc) error
	// ProjectProgress remembers the text as progress of the project.
	ProjectProgress(ctx context.Context, run *workspace.Run, project workspace.Item, text, actionID string, auto bool) error
	// Artifact records that the run wrote text into a part of an item.
	Artifact(ctx context.Context, run, item, kind, id, text string) error
	SaveItem(ctx context.Context, item workspace.Item) error
	// Actor is who adopts: the owner, or the assistant for an automatic adoption.
	Actor(ctx context.Context) string
	// SaveAction saves the changed item as an action of the owner's log.
	SaveAction(ctx context.Context, item workspace.Item, summary string) error
	Sample(ctx context.Context, sample workspace.Sample) error
	RecordUse(ctx context.Context, event memory.UseEvent) error
}

// Adopt writes the result of a run where the owner chose: a document, a
// progress note, or subtasks. auto is an adoption that the run made itself
// when it finished; its output is adopted unchanged. An adoption of the
// owner records that the run's memories were used.
func Adopt(ctx context.Context, u AdoptUnit, run *workspace.Run, as, text, actionID string, auto bool, now time.Time) error {
	if err := CheckAdoption(*run, as, text); err != nil {
		return err
	}
	// A completed draft can be adopted again after undo, whose audit/version
	// changes do not alter its content. Generation fences the destination;
	// adoption still fences revoked or deleted input and disabled agents.
	agent, err := u.Agent(ctx, run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	if err := u.VerifyAccess(ctx, *run); err != nil {
		return err
	}
	item, err := u.Item(ctx, run.ThingID)
	if err != nil {
		return err
	}
	if err := u.ClearAdoption(ctx, run.ID); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	switch as {
	case "doc":
		if run.Kind == "revise" {
			if err := u.Revise(ctx, *run, text); err != nil {
				return err
			}
		} else if err := u.SaveDoc(ctx, workspace.Doc{ID: string(memory.NewID()), ThingID: item.ID, Title: item.Title + " · " + run.Kind, Body: text, By: "ai", RunID: run.ID, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
	case "progress":
		switch item.Kind {
		case "project":
			if err := u.ProjectProgress(ctx, run, item, text, actionID, auto); err != nil {
				return err
			}
		case "task":
			item.Notes += "\n" + text
			if err := u.Artifact(ctx, run.ID, item.ID, "notes", run.ID, text); err != nil {
				return err
			}
		default:
			item.Body += "\n" + text
			if err := u.Artifact(ctx, run.ID, item.ID, "body", run.ID, text); err != nil {
				return err
			}
		}
	case "subtasks":
		lines, err := Subtasks(text)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if item.Kind == "task" {
				check := workspace.Check{ID: string(memory.NewID()), Text: line}
				item.Checklist = append(item.Checklist, check)
				if err := u.Artifact(ctx, run.ID, item.ID, "check", check.ID, line); err != nil {
					return err
				}
				continue
			}
			task := workspace.NewItem("task", line)
			task.History[0].By = u.Actor(ctx)
			task.Evolution[0].By = u.Actor(ctx)
			if item.Kind == "project" {
				task.ProjectID = item.ID
			} else {
				task.IdeaID = item.ID
				task.ProjectID = item.ProjectID
			}
			if err := u.SaveItem(ctx, task); err != nil {
				return err
			}
			if err := u.Artifact(ctx, run.ID, task.ID, "task", task.ID, line); err != nil {
				return err
			}
		}
	}
	item.Version++
	item.UpdatedAt = stamp
	if err := u.SaveAction(ctx, item, "采纳副手结果"); err != nil {
		return err
	}
	run.Adopted = &workspace.Adoption{As: as, At: stamp, Edited: !auto && text != run.Output, Auto: auto, ActionID: actionID}
	if err := u.Sample(ctx, workspace.Sample{ID: string(memory.NewID()), Kind: "adopted-result", Prompt: run.Brief, Response: text, Origin: workspace.Origin{Label: item.Title, RunID: run.ID}, Version: 1, State: "candidate", Epistemic: "inferred", CreatedAt: stamp}); err != nil {
		return err
	}
	if auto {
		return nil
	}
	for _, ref := range run.ContextVersions {
		// A use that is already recorded, or of a memory that is gone, is no failure.
		err := u.RecordUse(ctx, memory.UseEvent{Ref: ref, EventID: run.ID + ":" + string(ref.ID), Kind: "adoption", At: now})
		if err != nil && !errors.Is(err, memory.ErrConflict) && !errors.Is(err, memory.ErrNotFound) {
			return err
		}
	}
	return nil
}

// AutoAdoptable is true when a finished run adopts its own output: it is
// done, its context is current, it is not adopted, and its output says
// something.
func AutoAdoptable(run workspace.Run) bool {
	return run.Status == "done" && !run.StaleContext && run.Adopted == nil && TrimChecklist(run.Output) != ""
}

// AdoptionSummary is the line of the owner's log for an automatic adoption.
func AdoptionSummary(run workspace.Run, item workspace.Item, as string) string {
	switch {
	case as == "subtasks":
		return fmt.Sprintf("副手结果：加了 %d 个子任务", len(Checklist(run.Output)))
	case as == "progress" && item.Kind == "project":
		return "副手结果：记住项目进展"
	case as == "progress":
		return "副手结果：写进进度"
	}
	return "副手结果：存成文档"
}

// RevisionRejection is why the text that the owner pasted for a revision is
// not saved as a version, or empty. base is the version that the revision
// started from, or nil when it is gone; current is the document's text now.
func RevisionRejection(text string, base *string, current string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "改写正文为空"
	}
	if base == nil {
		return "基准文档已删除或版本不存在"
	}
	baseBody := strings.TrimSpace(*base)
	if text == baseBody {
		return "改写正文与基准完全相同"
	}
	if text == strings.TrimSpace(current) {
		return "改写正文与当前版本完全相同"
	}
	if len([]rune(baseBody)) >= 40 && len([]rune(text))*4 < len([]rune(baseBody)) {
		return "改写正文明显短于基准，疑似截断；未存为新版本"
	}
	return ""
}
