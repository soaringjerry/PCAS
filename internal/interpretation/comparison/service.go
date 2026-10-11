package comparison

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Stage is the queue stage of a statement comparison job. One job's stage is
// "memory.compare:<rule version>:<nonce>".
const Stage = "memory.compare"

// Store opens the job's session.
type Store interface {
	// Rejudge handles a job that judges one superseded statement again.
	// That path is still in storage.
	Rejudge(ctx context.Context, job worker.Job) error
	// Session holds the one connection that scans and comparisons share.
	Session(ctx context.Context) (session Session, release func(), err error)
	// Reschedule queues the next comparison when work remains. Storage records
	// a scheduling failure; it does not fail the finished job.
	Reschedule(ctx context.Context)
}

// Session is the set of reads and writes of one job.
type Session interface {
	// Ready returns the queue's deferral when no provider can generate text.
	Ready() error
	// NextBatch returns the input record of the next batch under the job's
	// lease. It applies the stage's hourly limit. When no batch remains it
	// acknowledges the job and reports found false.
	NextBatch(ctx context.Context, job worker.Job, version int) (record json.RawMessage, refs []memory.Ref, found bool, err error)
	// Apply writes the result of one batch in one transaction: the accepted
	// edges when the output was valid, the receipt of the batch in each case,
	// the removal of the saved result, and the acknowledgement.
	Apply(ctx context.Context, job worker.Job, record json.RawMessage, edges []Edge, valid bool) error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

// Service compares the statements of one batch.
type Service struct {
	Store     Store
	Generator Generator
	// Version is the current comparison rule version.
	Version int
}

// Process handles one comparison job. An invalid output writes no edge and is
// recorded with the batch, so that the same batch is not asked again unchanged.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	if strings.Contains(job.Stage, ":rejudge:") {
		return s.Store.Rejudge(ctx, job)
	}
	parts := strings.SplitN(job.Stage, ":", 3)
	if len(parts) != 3 {
		return memory.ErrInvalid
	}
	if v, err := strconv.Atoi(parts[1]); err != nil || v < 1 || s.Version < 1 {
		return memory.ErrInvalid
	}
	session, release, err := s.Store.Session(ctx)
	if err != nil {
		return err
	}
	defer release()
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	record, refs := json.RawMessage("null"), []memory.Ref{}
	if !saved {
		if err := session.Ready(); err != nil {
			return err
		}
		var found bool
		record, refs, found, err = session.NextBatch(ctx, job, s.Version)
		if err != nil || !found {
			return err
		}
	}
	record, output, err := s.Generator.GenerateForJob(ctx, job, "compare", prompts.Must("compare"), "", record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the batch that its call used.
	var batch struct {
		Memories []json.RawMessage `json:"memories"`
		Refs     []json.RawMessage `json:"refs"`
	}
	if err := json.Unmarshal(record, &batch); err != nil {
		return err
	}
	if len(batch.Memories) != len(batch.Refs) {
		return memory.ErrInvalid
	}
	edges, valid := ParseOutput(output, len(batch.Memories))
	if err := session.Apply(ctx, job, record, edges, valid); err != nil {
		return err
	}
	s.Store.Reschedule(ctx)
	return nil
}
