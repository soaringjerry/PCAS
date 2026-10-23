package topicproject

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const topicID = "11111111-1111-4111-8111-111111111111"

// fake is the storage and the model of one topic project job.
type fake struct {
	input        Input
	missing      bool
	changeDuring bool // the evidence changes while the model call runs
	output       string
	calls        int
	sent         Input
	existing     map[string]workspace.Item // by id
	createdToday int
	saved        []workspace.Item
	checks       []Output
	links        []string
	actions      []string
	nextPages    []int
	end          string
	discarded    int
}

func (f *fake) Input(context.Context, memory.ID, string) (Input, []memory.Ref, bool, error) {
	return f.input, []memory.Ref{{ID: "m1"}, {ID: "m2"}}, !f.missing, nil
}
func (f *fake) Drop(context.Context, worker.Job) error          { f.end = "dropped"; return nil }
func (f *fake) DiscardResult(context.Context, worker.Job) error { f.discarded++; return nil }
func (f *fake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *fake) Apply(_ context.Context, _ worker.Job, fn func(Unit) error) error { return fn(unit{f}) }
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, _ string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	f.calls++
	_ = json.Unmarshal(record, &f.sent)
	if f.changeDuring {
		f.input.Hash = "changed"
	}
	return record, f.output, nil
}

type unit struct{ *fake }

func (u unit) Input(string) (Input, bool, error) { return u.input, !u.missing, nil }
func (u unit) RecordCheck(_ string, _ Input, _ int, o Output) error {
	u.checks = append(u.checks, o)
	return nil
}
func (u unit) EnqueuePage(_ string, _ Input, page int) error {
	u.nextPages = append(u.nextPages, page)
	return nil
}
func (u unit) ProjectByName(name string) (string, bool, error) {
	for id, p := range u.existing {
		if strings.EqualFold(strings.TrimSpace(p.Title), name) {
			return id, true, nil
		}
	}
	return "", false, nil
}
func (u unit) Project(id string) (workspace.Item, error) {
	p, ok := u.existing[id]
	if !ok {
		return p, memory.ErrNotFound
	}
	return p, nil
}
func (u unit) ProjectsCreatedSince(time.Time) (int, error) { return u.createdToday, nil }
func (u unit) SaveProject(item workspace.Item) error       { u.saved = append(u.saved, item); return nil }
func (u unit) LinkTopic(_ string, _ Input, project, _ string, created bool) error {
	u.links = append(u.links, project+map[bool]string{true: ":created", false: ":attached"}[created])
	return nil
}
func (u unit) LogAction(_, summary string) error { u.actions = append(u.actions, summary); return nil }
func (u unit) DiscardResult() error              { u.discarded++; return nil }
func (u unit) Acknowledge() error                { u.end = "acknowledged"; return nil }

func fixture(output map[string]any) (*fake, Service, worker.Job) {
	data, _ := json.Marshal(output)
	f := &fake{output: string(data), existing: map[string]workspace.Item{}, input: Input{Topic: "Fictitious trip", Kind: "topic", Key: "entity:" + topicID, Hash: "h1", Timezone: "UTC", Version: 3,
		Projects: []Summary{{ID: "p1", Name: "Fictitious report"}}}}
	f.existing["p1"] = workspace.Item{ID: "p1", Title: "Fictitious report"}
	service := Service{Store: f, Generator: f, Now: func() time.Time { return time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC) }}
	return f, service, worker.Job{Stage: Stage + ":" + topicID + ":h1:0"}
}

func TestNewProposalCreatesOneProjectInTheSystemsNameWithItsEvidence(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": " Fictitious plan ", "goal": strings.Repeat("目", 450), "reason": "Fictitious reason."})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(f.saved) != 1 || f.calls != 1 || f.end != "acknowledged" || f.discarded != 1 || f.sent.Now != "2026-10-10" {
		t.Fatalf("%+v", f)
	}
	p := f.saved[0]
	if p.Kind != "project" || p.Title != "Fictitious plan" || len([]rune(p.Goal)) != GoalCharacters || p.History[0].By != "ai" || p.Creation == nil || p.Creation.By != "background_topic" || len(p.Creation.MemoryIDs) != 2 || p.Creation.GroupKey != f.input.Key {
		t.Fatalf("%+v", p)
	}
	if len(f.links) != 1 || f.links[0] != p.ID+":created" || len(f.actions) != 1 || f.actions[0] != "建了项目「 Fictitious plan 」" || len(f.checks) != 1 {
		t.Fatal(f.links, f.actions, f.checks)
	}
}

func TestWhatTheUserCallsAProjectKeepsTheUsersName(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": "A model's name", "goal": "", "reason": "Fictitious reason."})
	f.input.Kind = "project"
	if err := service.Process(context.Background(), job); err != nil || len(f.saved) != 1 || f.saved[0].Title != "Fictitious trip" {
		t.Fatal(err, f.saved)
	}
}

func TestMatchAttachesToAShownProjectAndNeverToAnother(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "match", "projectId": "p1", "name": "", "goal": "", "reason": "Fictitious reason."})
	if err := service.Process(context.Background(), job); err != nil || len(f.saved) != 0 || len(f.links) != 1 || f.links[0] != "p1:attached" || f.actions[0] != "归入已有项目「Fictitious report」" {
		t.Fatal(err, f.saved, f.links, f.actions)
	}
	f, service, job = fixture(map[string]any{"decision": "match", "projectId": "p9", "name": "", "goal": "", "reason": "Fictitious reason."})
	f.existing["p9"] = workspace.Item{ID: "p9", Title: "A project that the model did not see"}
	err := service.Process(context.Background(), job)
	var retry *worker.JobError
	if !errors.As(err, &retry) || retry.Code != "topic_project_output_invalid" || len(f.links) != 0 || len(f.checks) != 0 || f.discarded != 1 {
		t.Fatal(err, f.links, f.checks)
	}
}

func TestNewProposalWaitsUntilTheModelHasSeenEveryProject(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": "Fictitious plan", "goal": "", "reason": "Fictitious reason."})
	for i := 0; i < PageSize+5; i++ {
		f.input.Projects = append(f.input.Projects, Summary{ID: "x", Name: "Fictitious other"})
	}
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if len(f.sent.Projects) != PageSize || len(f.saved) != 0 || len(f.links) != 0 || len(f.nextPages) != 1 || f.nextPages[0] != 1 || f.end != "acknowledged" {
		t.Fatal(len(f.sent.Projects), f.saved, f.nextPages, f.end)
	}
}

func TestNewProposalWithAnExistingNameAttachesInsteadOfCreating(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": "fictitious REPORT", "goal": "", "reason": "Fictitious reason."})
	if err := service.Process(context.Background(), job); err != nil || len(f.saved) != 0 || f.links[0] != "p1:attached" {
		t.Fatal(err, f.saved, f.links)
	}
}

func TestDailyLimitDefersTheJobAndWritesNoProject(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": "Fictitious plan", "goal": "", "reason": "Fictitious reason."})
	f.createdToday = DailyLimit
	err := service.Process(context.Background(), job)
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || deferred.Code != "topic_project_daily_limit" || !deferred.NoAttempt || deferred.Until.IsZero() || len(f.saved) != 0 || len(f.links) != 0 || f.end != "" {
		t.Fatal(err, f.saved, f.end)
	}
}

func TestSkipIsRecordedAndCreatesNothing(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "skip", "projectId": nil, "name": "", "goal": "", "reason": "A past trip."})
	if err := service.Process(context.Background(), job); err != nil || len(f.checks) != 1 || f.checks[0].Reason != "A past trip." || len(f.saved)+len(f.links)+len(f.actions) != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.checks, f.end)
	}
}

func TestUncertainAndUnreadableOutputsAreTriedAgainWithoutAWrite(t *testing.T) {
	f, service, job := fixture(map[string]any{"decision": "uncertain", "projectId": nil, "name": "", "goal": "", "reason": "Fictitious reason."})
	err := service.Process(context.Background(), job)
	var retry *worker.JobError
	if !errors.As(err, &retry) || retry.Code != "topic_project_uncertain" || len(f.checks) != 0 {
		t.Fatal(err)
	}
	f, service, job = fixture(map[string]any{"decision": "new", "projectId": nil, "name": "Fictitious plan", "goal": "", "reason": "Fictitious reason.", "extra": true})
	if err := service.Process(context.Background(), job); !errors.As(err, &retry) || retry.Code != "topic_project_output_invalid" || len(f.saved) != 0 {
		t.Fatal(err)
	}
}

func TestChangedEvidenceDropsTheAnswer(t *testing.T) {
	// Changed before the call: no call is made.
	f, service, job := fixture(map[string]any{"decision": "new", "projectId": nil, "name": "Fictitious plan", "goal": "", "reason": "Fictitious reason."})
	f.input.Hash = "h2"
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.end != "dropped" {
		t.Fatal(err, f.calls, f.end)
	}
	// Changed during the call: the paid answer is not applied.
	f, service, job = fixture(map[string]any{"decision": "new", "projectId": nil, "name": "Fictitious plan", "goal": "", "reason": "Fictitious reason."})
	f.changeDuring = true
	if err := service.Process(context.Background(), job); err != nil || f.calls != 1 || len(f.saved)+len(f.checks) != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.saved, f.end)
	}
}

func TestTheInputOfATopicSaysWhereItCutText(t *testing.T) {
	if text, n := ClipMemory(strings.Repeat("长", MemoryCharacters+3)); n != 3 || !strings.HasSuffix(text, "（原文还有内容，拿不准请用 uncertain）") {
		t.Fatal(n, text[len(text)-30:])
	}
	if text, n := ClipGoal("短目标"); n != 0 || text != "短目标" {
		t.Fatal(n, text)
	}
}
