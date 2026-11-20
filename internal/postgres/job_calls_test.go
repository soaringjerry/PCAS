package postgres

import (
	"context"
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/modelcall/jobcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// jobCalls composes the background call generator as the application does.
// It reads the store's gateway at each call, because a test can set the
// models again after it composed a service.
func jobCalls(s *Store) lazyJobCalls { return lazyJobCalls{s} }

type lazyJobCalls struct{ store *Store }

func (l lazyJobCalls) generator() jobcall.Generator {
	return jobcall.Generator{Calls: l.store.Gateway(), Store: l.store.JobCallStore()}
}

func (l lazyJobCalls) HasSavedResult(ctx context.Context, job worker.Job) (bool, error) {
	return l.generator().HasSavedResult(ctx, job)
}

func (l lazyJobCalls) Generate(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (*modelcall.PaidResult, error) {
	return l.generator().Generate(ctx, job, purpose, instructions, modelInput, record, refs)
}

func (l lazyJobCalls) GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, error) {
	return l.generator().GenerateForJob(ctx, job, purpose, instructions, modelInput, record, refs)
}

func (l lazyJobCalls) GenerateWithRefs(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, []memory.Ref, error) {
	return l.generator().GenerateWithRefs(ctx, job, purpose, instructions, record, refs)
}

// indexer composes the vector indexer as the application does.
func indexer(s *Store) retrieval.Indexer {
	out := retrieval.Indexer{Store: s.IndexStore(), Calls: s.Gateway()}
	if models := registryOf(s); models != nil {
		out.Providers = models
	}
	return out
}
