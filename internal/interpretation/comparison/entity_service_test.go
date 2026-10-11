package comparison

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

// entityFake is the storage and the model of one entity comparison job.
type entityFake struct {
	noPair                                bool
	namesChanged, evidenceGone, lockFails bool
	output                                string
	calls                                 int
	merged                                *int
	receipt                               *bool
	invalid                               string
	log                                   []string
}

func (f *entityFake) Session(context.Context) (EntitySession, func(), error) {
	return f, func() {}, nil
}
func (f *entityFake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *entityFake) Ready() error { return nil }
func (f *entityFake) NextPair(context.Context, worker.Job, int) (json.RawMessage, []memory.Ref, bool, error) {
	if f.noPair {
		return nil, nil, false, nil
	}
	return json.RawMessage(`{"entities":[{"n":1,"type":"person","name":"Fictitious Wang","memoryCount":2},{"n":2,"type":"person","name":"虚构老王","memoryCount":2}]}`), []memory.Ref{{ID: "a"}, {ID: "b"}}, true, nil
}
func (f *entityFake) Finish(context.Context, worker.Job) error {
	f.log = append(f.log, "finish")
	return nil
}
func (f *entityFake) RecordInvalid(_ context.Context, _ worker.Job, _ int, _ json.RawMessage, _ []memory.Ref, output string) error {
	f.invalid = output
	return nil
}
func (f *entityFake) Apply(_ context.Context, _ worker.Job, _ int, _ json.RawMessage, _ []memory.Ref, merge bool, fn func(EntityUnit) error) error {
	if merge {
		f.log = append(f.log, "merge-limit")
	}
	return fn(entityUnit{f})
}
func (f *entityFake) HasSavedResult(context.Context, worker.Job) (bool, error) { return false, nil }
func (f *entityFake) GenerateWithRefs(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, []memory.Ref, error) {
	f.calls++
	return record, f.output, refs, nil
}

type entityUnit struct{ *entityFake }

func (u entityUnit) NamesUnchanged() (bool, error)  { return !u.namesChanged, nil }
func (u entityUnit) EvidenceCurrent() (bool, error) { return !u.evidenceGone, nil }
func (u entityUnit) LockEntities() (bool, error)    { return !u.lockFails, nil }
func (u entityUnit) Merge(keep int) error           { u.merged = &keep; return nil }
func (u entityUnit) Event(outcome, reason string, _ int) error {
	u.log = append(u.log, outcome+":"+reason)
	return nil
}
func (u entityUnit) Receipt(same *bool, _ string) error { u.receipt = same; return nil }
func (u entityUnit) DiscardResult() error               { u.log = append(u.log, "discard"); return nil }
func (u entityUnit) Acknowledge() error                 { u.log = append(u.log, "acknowledged"); return nil }

var entityJob = worker.Job{Stage: EntityStage + ":2:nonce"}

func entityService(f *entityFake) EntityService {
	return EntityService{Store: f, Generator: f, Version: 2}
}

func TestSameEntityMergesIntoTheKeeperOfTheRuleNotOfTheModel(t *testing.T) {
	// The model keeps the first name. The counts are equal, so the Chinese name is kept.
	f := &entityFake{output: `{"same":true,"keep":1}`}
	if err := entityService(f).Process(context.Background(), entityJob); err != nil {
		t.Fatal(err)
	}
	if f.merged == nil || *f.merged != 1 || f.receipt == nil || !*f.receipt || !reflect.DeepEqual(f.log, []string{"merge-limit", "discard", "acknowledged"}) {
		t.Fatal(f.merged, f.receipt, f.log)
	}
}

func TestDifferentEntitiesAreRecordedWithoutAMerge(t *testing.T) {
	f := &entityFake{output: `{"same":false,"keep":null}`, evidenceGone: true}
	if err := entityService(f).Process(context.Background(), entityJob); err != nil || f.merged != nil || f.receipt == nil || *f.receipt || !reflect.DeepEqual(f.log, []string{"discard", "acknowledged"}) {
		t.Fatal(err, f.merged, f.receipt, f.log)
	}
}

func TestMergeNeedsUnchangedNamesAndCurrentEvidenceAtWriteTime(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(*entityFake)
		log  []string
	}{
		"a name changed during the call": {func(f *entityFake) { f.namesChanged = true }, []string{"merge-limit", "discard", "acknowledged"}},
		"a sampled memory is gone":       {func(f *entityFake) { f.evidenceGone = true }, []string{"merge-limit", "deferred:identity_input_changed", "discard", "acknowledged"}},
		"a name changed under the lock":  {func(f *entityFake) { f.lockFails = true }, []string{"merge-limit", "deferred:identity_name_changed", "discard", "acknowledged"}},
	} {
		f := &entityFake{output: `{"same":true,"keep":2}`}
		c.edit(f)
		if err := entityService(f).Process(context.Background(), entityJob); err != nil || f.merged != nil || f.receipt != nil || !reflect.DeepEqual(f.log, c.log) {
			t.Error(name, err, f.merged, f.receipt, f.log)
		}
	}
}

func TestUnreadableEntityAnswerIsRecordedAndTriedAgain(t *testing.T) {
	for _, output := range []string{`{"same":true}`, `{"same":true,"keep":3}`, `{"keep":1}`, `Fictitious prose.`} {
		f := &entityFake{output: output}
		err := entityService(f).Process(context.Background(), entityJob)
		var retry *worker.JobError
		if !errors.As(err, &retry) || retry.Code != "invalid_entity_compare_output" || f.invalid != output || f.merged != nil {
			t.Error(output, err, f.invalid)
		}
	}
}

func TestEntityComparisonWithoutAPairSubmitsNothing(t *testing.T) {
	f := &entityFake{noPair: true}
	if err := entityService(f).Process(context.Background(), entityJob); err != nil || f.calls != 0 || !reflect.DeepEqual(f.log, []string{"finish"}) {
		t.Fatal(err, f.calls, f.log)
	}
}
