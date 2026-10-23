// Package datereview owns the current-state rule for a remembered date that is
// not a time to show: one that was never pinned down, a fixed arrangement
// without an hour, or one that is over. The model proposes to keep it, drop
// it, or turn it into a task or an idea. This package validates the proposal
// and decides what is written. Workspace items and the memory date are changed
// through their owners' commands in one recorded action.
package datereview

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Stage is the queue stage. One job's stage is "memory.date_tidy:<class>".
const Stage = "memory.date_tidy"

// HourlyCalls is the number of date reviews that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 30

// Entry is the date under review. Its JSON form is part of the model input.
type Entry struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	At           string `json:"at"`
	Recurrence   string `json:"recurrence"`
	TimeNote     string `json:"timeNote"`
	OriginalText string `json:"originalText"`
	Memory       string `json:"memory"`
	SaidOn       string `json:"saidOn"`
	Acquisition  string `json:"acquisition"`
}

// Input is the model input for one date.
type Input struct {
	Now       string   `json:"now"`
	Timezone  string   `json:"timezone"`
	Class     string   `json:"class"`
	Entry     Entry    `json:"entry"`
	SameTitle []Same   `json:"sameTitle"`
	OpenTasks []string `json:"openTasks"`
	Ideas     []string `json:"ideas"`
}

// Same is another open date with the same title.
type Same struct {
	At           string `json:"at"`
	SaidOn       string `json:"saidOn"`
	OriginalText string `json:"originalText"`
}

// Output is the model's proposal.
type Output struct {
	Decision string `json:"decision"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
}

// Store reads and writes the records of one date review job.
type Store interface {
	// Input assembles the date's current input. open is false when the date was
	// closed, retired, edited, or already judged since the job was queued.
	// clipped is the number of characters that the input leaves out.
	Input(ctx context.Context, job worker.Job, class string) (input Input, open bool, clipped int, err error)
	// Drop discards the job's saved result and acknowledges the job.
	Drop(ctx context.Context, job worker.Job) error
	DiscardResult(ctx context.Context, job worker.Job) error
	RetryLater(job worker.Job, code string) error
	// Apply runs fn in one transaction that holds the owner and the job's lease.
	Apply(ctx context.Context, job worker.Job, fn func(Unit) error) error
}

// Unit is the set of reads and writes that one Apply transaction permits.
type Unit interface {
	Input(class string) (input Input, open bool, err error)
	// Event records a visible stage outcome with its reason.
	Event(outcome, reason string, count int) error
	// Act records one undoable action with the given label and runs fn in it.
	Act(label string, fn func(Action) error) (actionID string, err error)
	RecordDecision(class string, input Input, output Output, itemID, actionID *string) error
	DiscardResult() error
	Acknowledge() error
}

// Action is the set of owner commands that one recorded action permits.
type Action interface {
	// ID is the identity of the recorded action.
	ID() string
	// SaveItem creates a workspace item through the workspace item rules.
	SaveItem(workspace.Item) error
	// CloseDate closes the reviewed date through the memory date command.
	// as is "dropped" or "task".
	CloseDate(as string) error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

type Service struct {
	Store     Store
	Generator Generator
}

// Process handles one date review job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	parts := strings.Split(job.Stage, ":")
	if len(parts) != 2 || (parts[1] != "unclear" && parts[1] != "habit" && parts[1] != "past") {
		return memory.ErrInvalid
	}
	class := parts[1]
	input, open, clipped, err := s.Store.Input(ctx, job, class)
	if err != nil {
		return err
	}
	if !open {
		return s.Store.Drop(ctx, job)
	}
	record, _ := json.Marshal(input)
	ref := memory.Ref{ID: job.Record.ID, Version: job.Record.Version, Kind: memory.ClaimKind}
	_, text, err := s.Generator.GenerateForJob(ctx, job, "date_tidy", prompts.Must("date-tidy"), "", record, []memory.Ref{ref})
	if err != nil {
		return err
	}
	output, valid := Validate(text)
	if !valid {
		if err := s.Store.DiscardResult(ctx, job); err != nil {
			return err
		}
		return s.Store.RetryLater(job, "date_tidy_output_invalid")
	}
	return s.Store.Apply(ctx, job, func(u Unit) error { return apply(u, job, class, input, clipped, output) })
}

// Validate reads a proposal. A task or an idea needs a valid title.
func Validate(text string) (Output, bool) {
	var output Output
	valid := modelcall.DecodeStrict([]byte(strings.TrimSpace(text)), &output) == nil && (output.Decision == "keep" || output.Decision == "drop" || output.Decision == "task" || output.Decision == "idea") && strings.TrimSpace(output.Reason) != ""
	output.Title = strings.TrimSpace(output.Title)
	if output.Decision == "task" || output.Decision == "idea" {
		valid = valid && workspace.ValidTitle(output.Title)
	}
	return output, valid
}

func apply(u Unit, job worker.Job, class string, input Input, clipped int, output Output) error {
	fresh, still, err := u.Input(class)
	if err != nil {
		return err
	}
	finish := func() error {
		if err := u.DiscardResult(); err != nil {
			return err
		}
		return u.Acknowledge()
	}
	if !still || fresh.Entry != input.Entry {
		// The date changed during the call. The proposal was about something
		// else. The next scheduling pass queues the new version.
		return finish()
	}
	if clipped > 0 {
		if err := u.Event("overflow", "date_tidy_characters_left_in_memory", clipped); err != nil {
			return err
		}
	}
	var actionID, itemID *string
	if output.Decision != "keep" {
		label := map[string]string{"drop": "不再显示：" + input.Entry.Title, "task": "建了待办「" + output.Title + "」", "idea": "建了想法「" + output.Title + "」"}[output.Decision]
		id, err := u.Act(label, func(a Action) error {
			as := "dropped"
			if output.Decision != "drop" {
				as = "task"
				item := workspace.NewItem(output.Decision, output.Title)
				item.History[0].By = "ai"
				item.Evolution[0].By = "ai"
				if output.Decision == "task" && input.Entry.OriginalText != "" {
					item.Notes = "你在 " + input.Entry.SaidOn + " 说过：" + input.Entry.OriginalText
				}
				item.Creation = &workspace.ItemCreation{By: "background_tidy", ActionID: a.ID(), MemoryIDs: []string{string(job.Record.ID)}}
				if err := a.SaveItem(item); err != nil {
					return err
				}
				itemID = &item.ID
			}
			return a.CloseDate(as)
		})
		if err != nil {
			return err
		}
		actionID = &id
	}
	// A kept date is recorded too, so that it is not reviewed again unchanged.
	if err := u.RecordDecision(class, input, output, itemID, actionID); err != nil {
		return err
	}
	return finish()
}

// TidiedSummary is the home page's line for the dates that a review put away.
func TidiedSummary(n int) string {
	return fmt.Sprintf("收拾了日程：%d 条过了期或不作数的，不再显示", n)
}
