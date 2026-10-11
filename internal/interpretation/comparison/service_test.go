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

func (f *fake) Rejudge(context.Context, worker.Job) error {
	f.log = append(f.log, "rejudge")
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

func TestRejudgeJobGoesToItsOwnPath(t *testing.T) {
	f := &fake{}
	if err := service(f).Process(context.Background(), worker.Job{Stage: Stage + ":2:rejudge:claim"}); err != nil || !reflect.DeepEqual(f.log, []string{"rejudge"}) {
		t.Fatal(err, f.log)
	}
}
