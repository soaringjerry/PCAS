package datereview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fake is the storage and the model of one date review job.
type fake struct {
	input        Input
	closed       bool
	clipped      int
	changeDuring bool // the date is edited while the model call runs
	output       string
	calls        int
	log          []string
	items        []workspace.Item
	decision     *Output
	itemID       *string
	actionID     *string
	end          string
}

func (f *fake) Input(context.Context, worker.Job, string) (Input, bool, int, error) {
	return f.input, !f.closed, f.clipped, nil
}
func (f *fake) Drop(context.Context, worker.Job) error { f.end = "dropped"; return nil }
func (f *fake) DiscardResult(context.Context, worker.Job) error {
	f.log = append(f.log, "discard")
	return nil
}
func (f *fake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *fake) Apply(_ context.Context, _ worker.Job, fn func(Unit) error) error { return fn(unit{f}) }
func (f *fake) GenerateForJob(context.Context, worker.Job, string, prompts.Definition, string, json.RawMessage, []memory.Ref) (json.RawMessage, string, error) {
	f.calls++
	if f.changeDuring {
		f.input.Entry.Title = "Fictitious edited title"
	}
	return nil, f.output, nil
}

type unit struct{ *fake }

func (u unit) Input(string) (Input, bool, error) { return u.input, !u.closed, nil }
func (u unit) Event(outcome, reason string, _ int) error {
	u.log = append(u.log, outcome+":"+reason)
	return nil
}
func (u unit) Act(label string, fn func(Action) error) (string, error) {
	u.log = append(u.log, "action:"+label)
	return "action-1", fn(action{u.fake})
}
func (u unit) RecordDecision(_ string, _ Input, o Output, itemID, actionID *string) error {
	u.decision, u.itemID, u.actionID = &o, itemID, actionID
	return nil
}
func (u unit) DiscardResult() error { u.log = append(u.log, "discard"); return nil }
func (u unit) Acknowledge() error   { u.end = "acknowledged"; return nil }

type action struct{ *fake }

func (a action) ID() string                      { return "action-1" }
func (a action) SaveItem(i workspace.Item) error { a.items = append(a.items, i); return nil }
func (a action) CloseDate(as string) error       { a.log = append(a.log, "close:"+as); return nil }

func fixture(output map[string]any) (*fake, Service, worker.Job) {
	data, _ := json.Marshal(output)
	f := &fake{output: string(data), input: Input{Class: "past", Entry: Entry{Title: "Fictitious dentist", OriginalText: "Fictitious original sentence.", SaidOn: "2026-09-30"}}}
	job := worker.Job{Stage: Stage + ":past", Record: memory.Ref{ID: "claim-1", Version: 2}}
	return f, Service{Store: f, Generator: f}, job
}

func TestTaskProposalCreatesOneTaskAndClosesTheDateInOneAction(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "task", "title": " Fictitious call ", "reason": "Still to do."})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(f.items) != 1 || f.end != "acknowledged" || !reflect.DeepEqual(f.log, []string{"action:建了待办「Fictitious call」", "close:task", "discard"}) {
		t.Fatal(f.items, f.log, f.end)
	}
	item := f.items[0]
	if item.Kind != "task" || item.Title != "Fictitious call" || item.Notes != "你在 2026-09-30 说过：Fictitious original sentence." || item.History[0].By != "ai" || item.Creation == nil || item.Creation.By != "background_tidy" || item.Creation.ActionID != "action-1" || !reflect.DeepEqual(item.Creation.MemoryIDs, []string{"claim-1"}) {
		t.Fatalf("%+v", item)
	}
	if f.decision.Decision != "task" || f.itemID == nil || *f.itemID != item.ID || f.actionID == nil || *f.actionID != "action-1" {
		t.Fatal(f.decision, f.itemID, f.actionID)
	}
}

func TestIdeaAndDropProposals(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "idea", "title": "Fictitious idea", "reason": "Not a commitment."})
	if err := service.Process(context.Background(), job); err != nil || len(f.items) != 1 || f.items[0].Kind != "idea" || f.items[0].Notes != "" || f.log[1] != "close:task" {
		t.Fatal(err, f.items, f.log)
	}
	f, service, job = fixture(map[string]any{"decision": "drop", "title": "", "reason": "It is over."})
	if err := service.Process(context.Background(), job); err != nil || len(f.items) != 0 || !reflect.DeepEqual(f.log, []string{"action:不再显示：Fictitious dentist", "close:dropped", "discard"}) || f.itemID != nil || f.actionID == nil {
		t.Fatal(err, f.items, f.log)
	}
}

func TestKeepIsRecordedWithoutAnAction(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "keep", "title": "", "reason": "Still a time to show."})
	f.clipped = 40
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"overflow:date_tidy_characters_left_in_memory", "discard"}) || f.decision == nil || f.decision.Reason != "Still a time to show." || f.actionID != nil || f.end != "acknowledged" {
		t.Fatal(f.log, f.decision, f.actionID)
	}
}

func TestInvalidProposalIsTriedAgainWithoutAWrite(t *testing.T) {
	for name, output := range map[string]map[string]any{
		"unknown decision":   {"decision": "archive", "title": "", "reason": "Fictitious."},
		"task without title": {"decision": "task", "title": " ", "reason": "Fictitious."},
		"no reason":          {"decision": "drop", "title": "", "reason": " "},
		"unknown field":      {"decision": "keep", "title": "", "reason": "Fictitious.", "extra": 1},
	} {
		t.Run(name, func(t *testing.T) {
			f, service, job := fixture(output)
			err := service.Process(context.Background(), job)
			var retry *worker.JobError
			if !errors.As(err, &retry) || retry.Code != "date_tidy_output_invalid" || f.decision != nil || len(f.items) != 0 || f.end != "" {
				t.Fatal(err, f.decision, f.end)
			}
		})
	}
}

func TestClosedOrEditedDateGetsNoVerdict(t *testing.T) {
	// Closed before the call: no call is made.
	f, service, job := fixture(map[string]any{"decision": "drop", "title": "", "reason": "It is over."})
	f.closed = true
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.end != "dropped" {
		t.Fatal(err, f.calls, f.end)
	}
	// Edited during the call: the proposal was about something else.
	f, service, job = fixture(map[string]any{"decision": "drop", "title": "", "reason": "It is over."})
	f.changeDuring = true
	if err := service.Process(context.Background(), job); err != nil || f.calls != 1 || f.decision != nil || !reflect.DeepEqual(f.log, []string{"discard"}) || f.end != "acknowledged" {
		t.Fatal(err, f.log, f.decision)
	}
	job.Stage = Stage + ":someday"
	if err := service.Process(context.Background(), job); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}
