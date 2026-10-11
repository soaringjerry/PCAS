package projecthandover

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

const projectID = "11111111-1111-4111-8111-111111111111"

var (
	itemEvidence   = workspace.ProjectEvidence{Kind: "item", ID: "task-1"}
	memoryEvidence = workspace.ProjectEvidence{Kind: "memory", ID: "claim-1", Version: 2}
)

// fake is the storage and the model of one project handover job.
type fake struct {
	project    *workspace.Item
	existing   workspace.ProjectHandover
	input      Input
	saved      json.RawMessage // a paid result that waits from an earlier attempt
	output     string
	calls      int
	gone       map[workspace.ProjectEvidence]bool // evidence that no longer exists at write time
	written    *workspace.ProjectHandover
	events     []string
	end        string
	discarded  int
	inputReads int
}

func (f *fake) Exclusive(context.Context) (func(), error) { return func() {}, nil }
func (f *fake) Read(_ context.Context, _ worker.Job, fn func(Reader) error) error {
	return fn(unit{f})
}
func (f *fake) Apply(_ context.Context, _ worker.Job, fn func(Unit) error) error { return fn(unit{f}) }
func (f *fake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *fake) HasSavedResult(context.Context, worker.Job) (bool, error) { return f.saved != nil, nil }
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, _ string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	if f.saved != nil {
		return f.saved, f.output, nil
	}
	f.calls++
	return record, f.output, nil
}

type unit struct{ *fake }

func (u unit) Project(string) (workspace.Item, bool, error) {
	if u.project == nil {
		return workspace.Item{}, false, nil
	}
	return *u.project, true, nil
}
func (u unit) Handover(string) (workspace.ProjectHandover, error) { return u.existing, nil }
func (u unit) Input(string) (Input, []memory.Ref, error) {
	u.inputReads++
	return u.input, nil, nil
}
func (u unit) CurrentEvidence(input Input, h workspace.ProjectHandover) (Input, error) {
	current := Input{Project: input.Project}
	seen := map[workspace.ProjectEvidence]bool{}
	for _, section := range [][]workspace.ProjectSentence{h.Conclusion, h.Blockers, h.NextSteps} {
		for _, sentence := range section {
			for _, ref := range sentence.Evidence {
				if !seen[ref] && !u.gone[ref] {
					current.Parts = append(current.Parts, Part{Evidence: ref})
				}
				seen[ref] = true
			}
		}
	}
	return current, nil
}
func (u unit) Event(outcome, reason string, _ int) error {
	u.events = append(u.events, outcome+":"+reason)
	return nil
}
func (u unit) SaveHandover(_ string, h workspace.ProjectHandover, _ Input) error {
	u.written = &h
	return nil
}
func (u unit) DiscardResult() error { u.discarded++; return nil }
func (u unit) Acknowledge() error   { u.end = "acknowledged"; return nil }

func sentence(text string, evidence ...workspace.ProjectEvidence) map[string]any {
	return map[string]any{"text": text, "evidence": evidence}
}

func fixture(sections map[string]any) (*fake, Service, worker.Job) {
	data, _ := json.Marshal(sections)
	f := &fake{output: string(data), project: &workspace.Item{ID: projectID, Kind: "project", Status: "active"}, existing: Empty(projectID), gone: map[workspace.ProjectEvidence]bool{},
		input: Input{Project: workspace.Item{ID: projectID}, Parts: []Part{{Evidence: itemEvidence}, {Evidence: memoryEvidence}}, Events: 7, Hash: "h1"}}
	return f, Service{Store: f, Generator: f}, worker.Job{Stage: Stage + ":" + projectID + ":nonce"}
}

func TestHandoverKeepsOnlySentencesWithEvidenceFromItsInput(t *testing.T) {
	f, service, job := fixture(map[string]any{
		"conclusion": []any{sentence("Fictitious draft is written.", itemEvidence), sentence("Fictitious claim without evidence."), sentence("Fictitious invented source.", workspace.ProjectEvidence{Kind: "memory", ID: "other"})},
		"blockers":   []any{},
		"nextSteps":  []any{sentence("Fictitious review comes next.", memoryEvidence)},
	})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.written == nil || len(f.written.Conclusion) != 1 || len(f.written.NextSteps) != 1 || len(f.written.Blockers) != 0 || !reflect.DeepEqual(f.events, []string{"overflow:ungrounded_sentences"}) || f.end != "acknowledged" || f.calls != 1 {
		t.Fatalf("%+v events=%v", f.written, f.events)
	}
}

func TestSentenceWhoseEvidenceIsGoneAtWriteTimeIsNotSaved(t *testing.T) {
	f, service, job := fixture(map[string]any{
		"conclusion": []any{sentence("Fictitious draft is written.", itemEvidence)},
		"blockers":   []any{sentence("Fictitious memory says it waits.", memoryEvidence)},
		"nextSteps":  []any{},
	})
	f.gone[memoryEvidence] = true // the memory was corrected during the call
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.written == nil || len(f.written.Conclusion) != 1 || len(f.written.Blockers) != 0 || !reflect.DeepEqual(f.events, []string{"overflow:ungrounded_sentences"}) {
		t.Fatalf("%+v events=%v", f.written, f.events)
	}
}

func TestSectionKeepsFiveSentencesAndCountsTheRest(t *testing.T) {
	many := []any{}
	for i := 0; i < SectionSentences+2; i++ {
		many = append(many, sentence("Fictitious step.", itemEvidence))
	}
	f, service, job := fixture(map[string]any{"conclusion": []any{}, "blockers": []any{}, "nextSteps": many})
	if err := service.Process(context.Background(), job); err != nil || f.written == nil || len(f.written.NextSteps) != SectionSentences || !reflect.DeepEqual(f.events, []string{"overflow:section_sentence_limit"}) {
		t.Fatal(err, f.written, f.events)
	}
}

func TestOutputWithoutAGroundedSentenceKeepsTheEarlierHandoverAndIsTriedAgain(t *testing.T) {
	for name, sections := range map[string]map[string]any{
		"no sentence":     {"conclusion": []any{}, "blockers": []any{}, "nextSteps": []any{}},
		"missing section": {"conclusion": []any{sentence("Fictitious.", itemEvidence)}, "blockers": []any{}},
		"only ungrounded": {"conclusion": []any{sentence("Fictitious.")}, "blockers": []any{}, "nextSteps": []any{}},
		"null section":    {"conclusion": nil, "blockers": []any{}, "nextSteps": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			f, service, job := fixture(sections)
			err := service.Process(context.Background(), job)
			var retry *worker.JobError
			if !errors.As(err, &retry) || retry.Code != "project_handover_invalid_output" || f.written != nil || f.discarded != 1 || f.events[len(f.events)-1] != "failure:project_handover_invalid_output" || f.end != "" {
				t.Fatal(err, f.written, f.events, f.end)
			}
		})
	}
}

func TestFinishedMissingOrCurrentProjectSubmitsNothing(t *testing.T) {
	valid := map[string]any{"conclusion": []any{sentence("Fictitious.", itemEvidence)}, "blockers": []any{}, "nextSteps": []any{}}
	f, service, job := fixture(valid)
	f.project.Status = "done"
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.end)
	}
	f, service, job = fixture(valid)
	f.project = nil
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.end)
	}
	// A handover that is written and not stale is current.
	f, service, job = fixture(valid)
	at := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339Nano)
	f.existing.WrittenAt = &at
	if err := service.Process(context.Background(), job); err != nil || f.calls != 0 || f.inputReads != 0 || f.end != "acknowledged" {
		t.Fatal(err, f.calls, f.end)
	}
}

func TestStaleHandoverIsRewrittenAtMostOnceAnHour(t *testing.T) {
	valid := map[string]any{"conclusion": []any{sentence("Fictitious.", itemEvidence)}, "blockers": []any{}, "nextSteps": []any{}}
	f, service, job := fixture(valid)
	recent := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	f.existing.WrittenAt, f.existing.Stale = &recent, true
	err := service.Process(context.Background(), job)
	var wait *worker.JobError
	if !errors.As(err, &wait) || wait.Code != "project_handover_interval" || !wait.NoAttempt || f.calls != 0 || time.Until(wait.Until) < 45*time.Minute {
		t.Fatal(err, f.calls)
	}
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	f.existing.WrittenAt = &old
	if err := service.Process(context.Background(), job); err != nil || f.calls != 1 || f.written == nil {
		t.Fatal(err, f.calls, f.written)
	}
}

func TestSavedResultIsCheckedAgainstItsOwnInputWithoutAnotherCall(t *testing.T) {
	f, service, job := fixture(map[string]any{"conclusion": []any{sentence("Fictitious.", itemEvidence)}, "blockers": []any{}, "nextSteps": []any{sentence("Fictitious memory step.", memoryEvidence)}})
	// The earlier attempt's input held the item only.
	f.saved, _ = json.Marshal(Input{Project: workspace.Item{ID: projectID}, Parts: []Part{{Evidence: itemEvidence}}, Events: 5, Hash: "h0"})
	if err := service.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 || f.inputReads != 0 || f.written == nil || len(f.written.Conclusion) != 1 || len(f.written.NextSteps) != 0 {
		t.Fatal(f.calls, f.inputReads, f.written)
	}
}
