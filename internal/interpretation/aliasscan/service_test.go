package aliasscan

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
)

func TestOnlyNamesOfTypesThatCanBeOneEntityArePaired(t *testing.T) {
	types := []string{"person", "person", "place", "topic", "organization", "event"}
	got := Pairs([][]int{{1, 2, 3}, {4, 3, 5}, {6, 6}, {2, 2}}, types)
	// Two persons pair. A person and a place do not. A topic pairs with a place
	// and with an organization, and a place does not pair with an organization.
	want := [][2]int{{1, 2}, {4, 3}, {4, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if PairTypesAllowed("event", "event") || !PairTypesAllowed("project", "project") || PairTypesAllowed("topic", "person") {
		t.Fatal("type rule changed")
	}
}

func TestGroupsMustNameEntitiesOfTheInput(t *testing.T) {
	if groups, valid := Validate(`{"groups":[[1,2],[3]]}`, 3); !valid || len(groups) != 2 {
		t.Fatal(groups, valid)
	}
	for _, output := range []string{`{"groups":[[1,4]]}`, `{"groups":[[0,1]]}`, `{"groups":null}`, `{"groups":[[1,2]],"extra":1}`, `not json`} {
		if _, valid := Validate(output, 3); valid {
			t.Error("accepted:", output)
		}
	}
	if _, valid := Validate(`{"groups":[]}`, 3); !valid {
		t.Error("an answer without groups is a valid answer")
	}
}

// fake is the storage and the model of one alias scan job.
type fake struct {
	noProvider bool
	noBatch    bool
	saved      json.RawMessage
	savedRefs  []memory.Ref
	output     string
	calls      int
	pairs      [][2]int
	log        []string
}

func (f *fake) Session(context.Context) (Session, func(), error) { return f, func() {}, nil }
func (f *fake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *fake) Ready() error {
	if f.noProvider {
		return &worker.JobError{Code: "provider_unavailable", NoAttempt: true}
	}
	return nil
}
func (f *fake) NextBatch(context.Context, worker.Job) (json.RawMessage, []memory.Ref, bool, error) {
	if f.noBatch {
		return nil, nil, false, nil
	}
	return json.RawMessage(`{"scope":"all","entities":[{"n":1,"type":"person","name":"Fictitious Ann"},{"n":2,"type":"person","name":"Fictitious Annie"},{"n":3,"type":"place","name":"Fictitious town"}],"marker":"m1"}`), []memory.Ref{{ID: "a"}, {ID: "b"}, {ID: "c"}}, true, nil
}
func (f *fake) Finish(context.Context, worker.Job) error { f.log = append(f.log, "finish"); return nil }
func (f *fake) DiscardResult(context.Context, worker.Job) error {
	f.log = append(f.log, "discard")
	return nil
}
func (f *fake) Apply(_ context.Context, _ worker.Job, _ json.RawMessage, _ []memory.Ref, fn func(Unit) error) error {
	return fn(unit{f})
}
func (f *fake) HasSavedResult(context.Context, worker.Job) (bool, error) { return f.saved != nil, nil }
func (f *fake) GenerateWithRefs(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, []memory.Ref, error) {
	if f.saved != nil {
		return f.saved, f.output, f.savedRefs, nil
	}
	f.calls++
	return record, f.output, refs, nil
}

type unit struct{ *fake }

func (u unit) ProposePair(a, b int) error { u.pairs = append(u.pairs, [2]int{a, b}); return nil }
func (u unit) MarkScanned() error         { u.log = append(u.log, "scanned"); return nil }
func (u unit) DiscardResult() error       { u.log = append(u.log, "discard"); return nil }
func (u unit) Acknowledge() error         { u.log = append(u.log, "acknowledged"); return nil }

var job = worker.Job{Stage: Stage + ":3:nonce"}

func TestScanProposesPairsAndMarksTheBatch(t *testing.T) {
	f := &fake{output: `{"groups":[[1,2,3]]}`}
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || !reflect.DeepEqual(f.pairs, [][2]int{{1, 2}}) || !reflect.DeepEqual(f.log, []string{"scanned", "discard", "acknowledged"}) {
		t.Fatal(f.calls, f.pairs, f.log)
	}
}

func TestScanWithoutABatchOrAProviderSubmitsNothing(t *testing.T) {
	f := &fake{noBatch: true}
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), job); err != nil || f.calls != 0 || !reflect.DeepEqual(f.log, []string{"finish"}) {
		t.Fatal(err, f.calls, f.log)
	}
	f = &fake{noProvider: true}
	err := (Service{Store: f, Generator: f}).Process(context.Background(), job)
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || deferred.Code != "provider_unavailable" || f.calls != 0 {
		t.Fatal(err, f.calls)
	}
	for _, stage := range []string{Stage, Stage + ":0:n", Stage + ":x:n", "memory.compare:3:n"} {
		if err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{Stage: stage}); !errors.Is(err, memory.ErrInvalid) {
			t.Error("accepted stage", stage, err)
		}
	}
}

func TestInvalidGroupsProposeNothingAndAreTriedAgain(t *testing.T) {
	f := &fake{output: `{"groups":[[1,9]]}`}
	err := (Service{Store: f, Generator: f}).Process(context.Background(), job)
	var retry *worker.JobError
	if !errors.As(err, &retry) || retry.Code != "invalid_entity_candidates_output" || len(f.pairs) != 0 || !reflect.DeepEqual(f.log, []string{"discard"}) {
		t.Fatal(err, f.pairs, f.log)
	}
}

func TestSavedResultIsAppliedToItsOwnNamesWithoutAnotherCall(t *testing.T) {
	// The earlier attempt saw two names. Its saved result must name two records.
	f := &fake{output: `{"groups":[[1,2]]}`, saved: json.RawMessage(`{"entities":[{"type":"topic"},{"type":"place"}],"marker":"m0"}`), savedRefs: []memory.Ref{{ID: "x"}, {ID: "y"}}}
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), job); err != nil || f.calls != 0 || !reflect.DeepEqual(f.pairs, [][2]int{{1, 2}}) {
		t.Fatal(err, f.calls, f.pairs)
	}
	f.savedRefs = f.savedRefs[:1]
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), job); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("a saved result with other records was applied", err)
	}
}
