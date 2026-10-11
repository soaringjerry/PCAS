package effort

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

// fakeWorkspace is the storage and the model of one estimation job.
type fakeWorkspace struct {
	project workspace.Item
	tasks   map[string]workspace.Item
	saved   json.RawMessage // a paid result that waits from an earlier attempt
	output  string
	calls   int
	inputs  []string
	events  []string
	end     string
	loads   []bool
}

func (f *fakeWorkspace) HasSavedResult(context.Context, worker.Job) (bool, error) {
	return f.saved != nil, nil
}
func (f *fakeWorkspace) GenerateForJob(_ context.Context, _ worker.Job, purpose string, _ prompts.Definition, input string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	if f.saved != nil {
		return f.saved, f.output, nil
	}
	f.calls++
	f.inputs = append(f.inputs, purpose+":"+input)
	return record, f.output, nil
}
func (f *fakeWorkspace) Load(_ context.Context, _ worker.Job, project string, wantTasks bool) (workspace.Item, []workspace.Item, bool, error) {
	f.loads = append(f.loads, wantTasks)
	if f.project.ID != project {
		f.end = "acknowledged"
		return workspace.Item{}, nil, true, nil
	}
	if !wantTasks {
		return f.project, nil, false, nil
	}
	tasks := []workspace.Item{}
	for _, id := range []string{taskA, taskB} {
		if t, ok := f.tasks[id]; ok && t.Due != "" && t.EstimatedHours == nil && t.EffortSource != "user" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		f.end = "acknowledged"
		return f.project, nil, true, nil
	}
	return f.project, tasks, false, nil
}
func (f *fakeWorkspace) Apply(_ context.Context, _ worker.Job, fn func(Unit) error) error {
	return fn(f)
}
func (f *fakeWorkspace) Location() (*time.Location, error) { return time.UTC, nil }
func (f *fakeWorkspace) Task(id string) (workspace.Item, bool, error) {
	t, ok := f.tasks[id]
	return t, ok, nil
}
func (f *fakeWorkspace) SaveTask(t workspace.Item) error { f.tasks[t.ID] = t; return nil }
func (f *fakeWorkspace) Event(outcome, reason string, count int) error {
	f.events = append(f.events, outcome+":"+reason)
	return nil
}
func (f *fakeWorkspace) DiscardResult() error      { f.saved = nil; return nil }
func (f *fakeWorkspace) Requeue(code string) error { f.end = "requeued:" + code; return nil }
func (f *fakeWorkspace) Acknowledge() error        { f.end = "acknowledged"; return nil }

const (
	projectID = "11111111-1111-4111-8111-111111111111"
	taskA     = "22222222-2222-4222-8222-222222222222"
	taskB     = "33333333-3333-4333-8333-333333333333"
)

func fixture() (*fakeWorkspace, Service, worker.Job) {
	f := &fakeWorkspace{project: workspace.Item{ID: projectID, Kind: "project", Title: "Fictitious project"}, tasks: map[string]workspace.Item{
		taskA: {ID: taskA, Kind: "task", ProjectID: projectID, Title: "Fictitious draft", Due: "2026-10-16T09:00:00Z", Version: 1},
		taskB: {ID: taskB, Kind: "task", ProjectID: projectID, Title: "Fictitious review", Due: "2026-10-20T09:00:00Z", Version: 1},
	}}
	service := Service{Store: f, Generator: f, Now: func() time.Time { return time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC) }}
	return f, service, worker.Job{Stage: Stage + ":" + projectID + ":nonce"}
}

func proposals(items ...map[string]any) string {
	data, _ := json.Marshal(map[string]any{"items": items})
	return string(data)
}

func TestEstimateIsAppliedToEachUnchangedTaskOnce(t *testing.T) {
	f, service, job := fixture()
	f.output = proposals(map[string]any{"itemId": taskA, "hours": 6, "reason": "Two fictitious sections."}, map[string]any{"itemId": taskB, "hours": 2, "reason": "One fictitious pass."})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	a := f.tasks[taskA]
	if f.calls != 1 || f.end != "acknowledged" || len(f.events) != 0 || a.EstimatedHours == nil || *a.EstimatedHours != 6 || a.EffortSource != "model" || a.EffortReason != "Two fictitious sections." || a.Version != 2 || len(a.History) != 1 || a.History[0].By != "system" {
		t.Fatalf("%+v %+v", f, a)
	}
	// With every task estimated, the next job has nothing to submit.
	f.end = ""
	if err := service.Process(context.Background(), job); err != nil || f.calls != 1 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.end)
	}
}

func TestEstimateNeverReplacesAUserEstimateOrAnEditedTask(t *testing.T) {
	for name, edit := range map[string]func(*workspace.Item){
		"user estimate":  func(t *workspace.Item) { h := 1.0; t.EstimatedHours, t.EffortSource = &h, "user" },
		"edited title":   func(t *workspace.Item) { t.Title = "Fictitious new title" },
		"moved project":  func(t *workspace.Item) { t.ProjectID = taskB },
		"edited duedate": func(t *workspace.Item) { t.Due = "2026-11-01T09:00:00Z" },
	} {
		t.Run(name, func(t *testing.T) {
			f, service, job := fixture()
			// The user changes the task while the model call is in progress.
			service.Generator = editDuringCall{f, func() { task := f.tasks[taskA]; edit(&task); f.tasks[taskA] = task }}
			f.output = proposals(map[string]any{"itemId": taskA, "hours": 6, "reason": "Fictitious."}, map[string]any{"itemId": taskB, "hours": 2, "reason": "Fictitious."})
			before := f.tasks[taskA]
			edit(&before)
			if err := service.Process(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.tasks[taskA], before) || f.tasks[taskB].EffortSource != "model" || f.end != "acknowledged" {
				t.Fatalf("%+v end=%s", f.tasks[taskA], f.end)
			}
		})
	}
}

type editDuringCall struct {
	*fakeWorkspace
	edit func()
}

func (e editDuringCall) GenerateForJob(ctx context.Context, job worker.Job, purpose string, d prompts.Definition, input string, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, error) {
	saved, output, err := e.fakeWorkspace.GenerateForJob(ctx, job, purpose, d, input, record, refs)
	e.edit()
	return saved, output, err
}

func TestInvalidProposalsAreCountedAndAMissingTaskIsTriedAgain(t *testing.T) {
	f, service, job := fixture()
	f.output = proposals(
		map[string]any{"itemId": taskA, "hours": 6, "reason": "Fictitious."},
		map[string]any{"itemId": taskA, "hours": 9, "reason": "Fictitious repeat."},
		map[string]any{"itemId": "44444444-4444-4444-8444-444444444444", "hours": 1, "reason": "Fictitious unknown task."},
		map[string]any{"itemId": taskB, "hours": 2, "reason": " "},
	)
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	want := []string{"overflow:effort_missing_items", "overflow:effort_invalid_items", "failure:effort_incomplete_output"}
	if *f.tasks[taskA].EstimatedHours != 6 || f.tasks[taskB].EstimatedHours != nil || !reflect.DeepEqual(f.events, want) || f.end != "requeued:effort_incomplete_output" {
		t.Fatalf("events=%v end=%s", f.events, f.end)
	}
}

func TestUnreadableOutputChangesNothingAndIsTriedAgain(t *testing.T) {
	f, service, job := fixture()
	f.output = "Fictitious prose, not the requested structure."
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.tasks[taskA].EstimatedHours != nil || f.tasks[taskB].EstimatedHours != nil || f.end != "requeued:effort_incomplete_output" || f.events[len(f.events)-1] != "failure:effort_incomplete_output" {
		t.Fatalf("events=%v end=%s", f.events, f.end)
	}
}

func TestSavedResultIsAppliedToItsOwnSnapshotWithoutAnotherCall(t *testing.T) {
	f, service, job := fixture()
	// An earlier attempt paid for a call that saw only the first task.
	f.saved, _ = json.Marshal(map[string]any{"snapshot": Snapshot{Project: f.project, Items: []workspace.Item{f.tasks[taskA]}, DailyHours: DailyHours}})
	f.output = proposals(map[string]any{"itemId": taskA, "hours": 3, "reason": "Fictitious."})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 || !reflect.DeepEqual(f.loads, []bool{false}) || *f.tasks[taskA].EstimatedHours != 3 || f.tasks[taskB].EstimatedHours != nil || f.saved != nil || f.end != "acknowledged" {
		t.Fatalf("calls=%d loads=%v end=%s", f.calls, f.loads, f.end)
	}
}

func TestMissingProjectAndMalformedStageSubmitNothing(t *testing.T) {
	f, service, job := fixture()
	f.project.ID = taskB
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.end)
	}
	job.Stage = Stage + ":not-an-id:nonce"
	if err := service.Process(context.Background(), job); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestStartDateCountsBackWorkdaysFromTheDueDay(t *testing.T) {
	hours := func(h float64) *float64 { return &h }
	for _, c := range []struct {
		due   string
		hours *float64
		want  string
	}{
		{"2026-10-16T09:00:00Z", hours(4), "2026-10-15"},  // Friday, one workday
		{"2026-10-16T09:00:00Z", hours(6), "2026-10-14"},  // two workdays
		{"2026-10-12T09:00:00Z", hours(4), "2026-10-09"},  // Monday counts back over the weekend
		{"2026-10-17T09:00:00Z", hours(4), "2026-10-16"},  // a Saturday deadline starts on Friday
		{"2026-10-17T09:00:00Z", hours(0), "2026-10-16"},  // no hours: the last workday
		{"2026-10-16T09:00:00Z", hours(20), "2026-10-09"}, // five workdays are one week
	} {
		got, err := StartDate(c.due, c.hours, time.UTC)
		if err != nil || got == nil || *got != c.want {
			t.Errorf("due=%s hours=%v got=%v want=%s err=%v", c.due, *c.hours, got, c.want, err)
		}
	}
	if got, err := StartDate("", hours(4), time.UTC); got != nil || err != nil {
		t.Fatal(got, err)
	}
	if _, err := StartDate("2026-10-16T09:00:00Z", hours(-1), time.UTC); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}
