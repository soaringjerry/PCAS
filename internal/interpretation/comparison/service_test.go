package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// fake is the storage and the model of one comparison job.
type fake struct {
	notReady, noBatch bool
	saved             json.RawMessage
	output            string
	calls             int
	edges             []Edge
	valid             *bool
	log               []string
}

func (f *fake) RejudgePair(_ context.Context, _ worker.Job, refs []memory.Ref) (json.RawMessage, bool, error) {
	f.log = append(f.log, "pair")
	if f.noBatch {
		return nil, false, nil
	}
	return json.RawMessage(`{"rule":2,"memories":[{"n":1},{"n":2}],"refs":[{"id":"a"},{"id":"b"}]}`), true, nil
}
func (f *fake) ApplyRejudge(_ context.Context, _ worker.Job, _ []memory.Ref, edges []Edge, valid bool) error {
	f.edges, f.valid = edges, &valid
	f.log = append(f.log, "rejudged")
	return nil
}
func (f *fake) Session(context.Context) (Session, func(), error) {
	return f, func() { f.log = append(f.log, "released") }, nil
}
func (f *fake) Reschedule(context.Context) { f.log = append(f.log, "rescheduled") }
func (f *fake) Ready() error {
	if f.notReady {
		return &worker.JobError{Code: "provider_unavailable", NoAttempt: true}
	}
	return nil
}
func (f *fake) NextBatch(context.Context, worker.Job, int) (json.RawMessage, []memory.Ref, bool, error) {
	if f.noBatch {
		return nil, nil, false, nil
	}
	return json.RawMessage(`{"rule":2,"memories":[{"n":1},{"n":2},{"n":3}],"refs":[{"id":"a"},{"id":"b"},{"id":"c"}]}`), nil, true, nil
}
func (f *fake) Apply(_ context.Context, _ worker.Job, _ json.RawMessage, edges []Edge, valid bool) error {
	f.edges, f.valid = edges, &valid
	f.log = append(f.log, "applied")
	return nil
}
func (f *fake) HasSavedResult(context.Context, worker.Job) (bool, error) { return f.saved != nil, nil }
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, _ string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	if f.saved != nil {
		return f.saved, f.output, nil
	}
	f.calls++
	return record, f.output, nil
}

var compareJob = worker.Job{Stage: Stage + ":2:nonce"}

func service(f *fake) Service { return Service{Store: f, Generator: f, Version: 2} }

func TestComparisonAppliesTheAcceptedEdgesAndQueuesTheNextBatch(t *testing.T) {
	f := &fake{output: `{"duplicates":[{"keep":1,"members":[1,2]}],"superseded":[]}`}
	if err := service(f).Process(context.Background(), compareJob); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || f.valid == nil || !*f.valid || !reflect.DeepEqual(f.edges, []Edge{{Old: 2, New: 1, Kind: "duplicate"}}) || !reflect.DeepEqual(f.log, []string{"applied", "rescheduled", "released"}) {
		t.Fatal(f.calls, f.edges, f.log)
	}
}

func TestUnreadableComparisonWritesNoEdgeAndIsRecordedWithItsBatch(t *testing.T) {
	f := &fake{output: "Fictitious prose."}
	if err := service(f).Process(context.Background(), compareJob); err != nil {
		t.Fatal(err)
	}
	if f.valid == nil || *f.valid || len(f.edges) != 0 || f.log[0] != "applied" {
		t.Fatal(f.valid, f.edges, f.log)
	}
}

func TestComparisonWithoutABatchOrAProviderSubmitsNothing(t *testing.T) {
	f := &fake{noBatch: true}
	if err := service(f).Process(context.Background(), compareJob); err != nil || f.calls != 0 || !reflect.DeepEqual(f.log, []string{"released"}) {
		t.Fatal(err, f.calls, f.log)
	}
	f = &fake{notReady: true}
	err := service(f).Process(context.Background(), compareJob)
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || deferred.Code != "provider_unavailable" || f.calls != 0 {
		t.Fatal(err)
	}
	for _, stage := range []string{Stage, Stage + ":0:n", Stage + ":x:n"} {
		if err := service(&fake{}).Process(context.Background(), worker.Job{Stage: stage}); !errors.Is(err, memory.ErrInvalid) {
			t.Error("accepted stage", stage, err)
		}
	}
}

func TestSavedComparisonIsAppliedToItsOwnBatchWithoutAnotherCall(t *testing.T) {
	// The earlier attempt's batch had two statements. A number above two is not valid for it.
	f := &fake{saved: json.RawMessage(`{"memories":[{"n":1},{"n":2}],"refs":[{"id":"a"},{"id":"b"}]}`), output: `{"duplicates":[],"superseded":[{"old":3,"new":1},{"old":2,"new":1}]}`, notReady: true}
	if err := service(f).Process(context.Background(), compareJob); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 || !reflect.DeepEqual(f.edges, []Edge{{Old: 2, New: 1, Kind: "superseded"}}) {
		t.Fatal(f.calls, f.edges)
	}
	f.saved = json.RawMessage(`{"memories":[{"n":1}],"refs":[]}`)
	if err := service(f).Process(context.Background(), compareJob); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("a saved batch with other records was applied", err)
	}
}

var rejudgeJob = worker.Job{Record: memory.Ref{ID: "11111111-1111-4111-8111-111111111111", Version: 3, Kind: memory.ClaimKind}, Stage: Stage + ":2:rejudge:11111111-1111-4111-8111-111111111111:3:22222222-2222-4222-8222-222222222222:1"}

func TestRejudgeAsksAboutItsPairAndAppliesACompleteVerdict(t *testing.T) {
	for output, want := range map[string][]Edge{
		`{"duplicates":[],"superseded":[{"old":1,"new":2}]}`:          {{Old: 1, New: 2, Kind: "superseded"}},
		`{"duplicates":[{"keep":2,"members":[1,2]}],"superseded":[]}`: {{Old: 1, New: 2, Kind: "duplicate"}},
		`{"duplicates":[],"superseded":[]}`:                           nil,
	} {
		f := &fake{output: output}
		if err := service(f).Process(context.Background(), rejudgeJob); err != nil {
			t.Fatal(err)
		}
		if f.calls != 1 || f.valid == nil || !*f.valid || len(f.edges) != len(want) || len(want) == 1 && f.edges[0] != want[0] || !reflect.DeepEqual(f.log, []string{"pair", "rejudged", "released"}) {
			t.Fatal(output, f.calls, f.edges, f.log)
		}
	}
}

func TestRejudgeWithAnIncompleteVerdictChangesNothing(t *testing.T) {
	for _, output := range []string{`{}`, "Fictitious prose.", `{"duplicates":[],"superseded":[{"old":2,"new":1}]}`, `{"duplicates":[{"keep":2,"members":[1,999]}],"superseded":[]}`} {
		f := &fake{output: output}
		if err := service(f).Process(context.Background(), rejudgeJob); err != nil {
			t.Fatal(err)
		}
		if f.valid == nil || *f.valid || len(f.edges) != 0 {
			t.Fatal("accepted", output, f.edges)
		}
	}
}

func TestRejudgeOfAChangedPairOrWithoutAProviderSubmitsNothing(t *testing.T) {
	f := &fake{noBatch: true}
	if err := service(f).Process(context.Background(), rejudgeJob); err != nil || f.calls != 0 || !reflect.DeepEqual(f.log, []string{"pair", "released"}) {
		t.Fatal(err, f.calls, f.log)
	}
	f = &fake{notReady: true}
	var deferred *worker.JobError
	if err := service(f).Process(context.Background(), rejudgeJob); !errors.As(err, &deferred) || deferred.Code != "provider_unavailable" || f.calls != 0 {
		t.Fatal(err)
	}
	// A saved result is applied to the pair of its own call, and only under the current rule.
	f = &fake{notReady: true, saved: json.RawMessage(`{"rule":2,"memories":[{"n":1},{"n":2}],"refs":[{"id":"a"},{"id":"b"}]}`), output: `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`}
	if err := service(f).Process(context.Background(), rejudgeJob); err != nil || f.calls != 0 || f.valid == nil || !*f.valid {
		t.Fatal(err, f.calls, f.log)
	}
	f = &fake{saved: json.RawMessage(`{"rule":1,"memories":[{"n":1},{"n":2}],"refs":[{"id":"a"},{"id":"b"}]}`), output: `{"duplicates":[],"superseded":[]}`}
	if err := service(f).Process(context.Background(), rejudgeJob); !errors.Is(err, memory.ErrInvalid) || f.valid != nil {
		t.Fatal("a result of another rule was applied", err)
	}
}

func TestRejudgeStageMustNameTheJobsRecordAndTheCurrentRule(t *testing.T) {
	other := rejudgeJob
	other.Record.Version = 4
	oldRule := rejudgeJob
	oldRule.Stage = Stage + ":1:rejudge:11111111-1111-4111-8111-111111111111:3:22222222-2222-4222-8222-222222222222:1"
	for _, job := range []worker.Job{other, oldRule, {Stage: Stage + ":2:rejudge:claim"}} {
		f := &fake{}
		if err := service(f).Process(context.Background(), job); !errors.Is(err, memory.ErrInvalid) || len(f.log) != 0 {
			t.Error("accepted", job.Stage, err, f.log)
		}
	}
}
