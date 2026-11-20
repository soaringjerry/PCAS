package organize

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Stage is the queue stage of an organize job. One job's stage is
// "memory.organize:<rule version>:<nonce>".
const Stage = "memory.organize"

// HourlyCalls is the number of classifications that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 40

// Batch is the model input that storage reads for one job.
type Batch struct {
	Memories []Memory
	Groups   []Group
	Timezone string
}

// Result is the read answer of one batch.
type Result struct {
	// Version is the rule version that the call used.
	Version  int
	Timezone string
	Memories []Memory
	Items    map[int]Item
	Declared []Group
}

// Applied reports what one result changed.
type Applied struct {
	// Deferred are the statements whose answer was not valid. They wait.
	Deferred []memory.ID
}

// Store opens the job's session.
type Store interface {
	// Session holds the one connection that organize and comparison calls
	// share. A second call at the same time gets the queue's deferral.
	Session(ctx context.Context) (session Session, release func(), err error)
}

// Session is the set of reads and writes of one job.
type Session interface {
	// Ready returns the queue's deferral when no provider can generate text.
	Ready() error
	// NextBatch returns the next statements under the job's lease. It applies
	// the stage's hourly limit. When no statement waits it acknowledges the
	// job and reports found false.
	NextBatch(ctx context.Context, job worker.Job, version int) (batch Batch, found bool, err error)
	// Apply writes one result in one transaction: the labels, groups, dates,
	// and rule scope of each statement that is still current, the wait of
	// each statement whose answer was not valid, the removal of the saved
	// result, the acknowledgement, and the next job when statements remain.
	Apply(ctx context.Context, job worker.Job, result Result) (Applied, error)
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

// Service organizes the statements of one batch.
type Service struct {
	Store     Store
	Generator Generator
	// Version is the current organize rule version.
	Version int
}

// Process handles one organize job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	session, release, err := s.Store.Session(ctx)
	if err != nil {
		return err
	}
	defer release()
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	batch := Batch{Memories: []Memory{}, Timezone: "UTC"}
	if !saved {
		if err := session.Ready(); err != nil {
			return err
		}
		var found bool
		batch, found, err = session.NextBatch(ctx, job, s.Version)
		if err != nil || !found {
			return err
		}
	}
	refs := []memory.Ref{}
	for _, m := range batch.Memories {
		refs = append(refs, m.Ref)
	}
	record, err := json.Marshal(map[string]any{"memories": batch.Memories, "groups": batch.Groups, "timezone": batch.Timezone, "refs": refs, "rule": s.Version})
	if err != nil {
		return err
	}
	record, output, err := s.Generator.GenerateForJob(ctx, job, "organize", prompts.Must("organize"), "", record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the batch and the rule that its call used.
	var snapshot struct {
		Memories []Memory     `json:"memories"`
		Timezone string       `json:"timezone"`
		Refs     []memory.Ref `json:"refs"`
		Rule     int          `json:"rule"`
	}
	if err := json.Unmarshal(record, &snapshot); err != nil {
		return err
	}
	// A result that was paid before rule versions were saved used rule two.
	if snapshot.Rule == 0 {
		snapshot.Rule = 2
	}
	if len(snapshot.Memories) != len(snapshot.Refs) {
		return memory.ErrInvalid
	}
	for i := range snapshot.Memories {
		snapshot.Memories[i].Ref = snapshot.Refs[i]
	}
	items, declared := ParseOutput(output, len(snapshot.Memories))
	applied, err := session.Apply(ctx, job, Result{Version: snapshot.Rule, Timezone: snapshot.Timezone, Memories: snapshot.Memories, Items: items, Declared: declared})
	if err != nil {
		return err
	}
	for _, id := range applied.Deferred {
		slog.WarnContext(ctx, "memory organization deferred after invalid output", "stage", "organize", "error_type", "invalid_output", "memory_id", id)
	}
	return nil
}
