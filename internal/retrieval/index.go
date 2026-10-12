package retrieval

import (
	"context"
	"errors"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// IndexStore is the storage of the vectors of one embedding job.
type IndexStore interface {
	// Missing returns, under no lock, the texts of the job's record that have
	// no vector of the model yet: the chunks of a source, or a memory as a
	// whole. When nothing is missing it finishes the job and reports found
	// false.
	Missing(ctx context.Context, job worker.Job, model string) (refs []memory.Ref, texts []string, found bool, err error)
	// Save writes the vectors under the job's lease and finishes the job. A
	// record version that is not current any more gets no vector.
	Save(ctx context.Context, job worker.Job, refs []memory.Ref, vectors []memory.Embedding) error
}

// Providers names the embedding provider.
type Providers interface {
	EmbeddingID() string
	Get(id string) (ai.Provider, bool)
	Available(id string) bool
}

// Indexer embeds the text of one record for the vector index.
type Indexer struct {
	Store IndexStore
	// Providers is nil when no model is configured.
	Providers Providers
	Calls     *modelcall.Gateway
}

// Process handles one embedding job. A paid call that failed is not submitted
// again; a free call can use the queue's retry.
func (s Indexer) Process(ctx context.Context, job worker.Job) error {
	if s.Providers == nil || s.Providers.EmbeddingID() == "" {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_not_configured"})
	}
	provider, ok := s.Providers.Get(s.Providers.EmbeddingID())
	if !ok {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_not_configured"})
	}
	if !s.Providers.Available(provider.ID) {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_unavailable", Retry: true})
	}
	refs, texts, found, err := s.Store.Missing(ctx, job, provider.ID+":"+provider.Model)
	if err != nil || !found {
		return err
	}
	var cost float64
	for _, text := range texts {
		cost += float64(len(text)+16) * provider.InputPerMillion / 1e6
	}
	// The call is its own execution. Its root and cause name the paying job.
	vectors, err := s.Calls.CallEmbedding(ctx, modelcall.DirectRequest{OwnerID: job.OwnerID, ExecutionID: memory.NewID(), RootExecutionID: job.ID, CausationID: job.ID, Function: "embedding", Stage: job.Stage, Provider: provider, Refs: refs, Estimate: cost, Job: &job}, texts)
	var failure *modelcall.Failure
	var persistence *modelcall.PersistenceError
	switch {
	case err == nil:
	case errors.As(err, &persistence):
		return err
	case errors.Is(err, modelcall.ErrNotAvailable), errors.Is(err, ai.ErrUnsupportedCapability), errors.As(err, &failure) && failure.Code == modelcall.ErrNotAvailable.Error():
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_unavailable", Retry: true})
	case errors.As(err, &failure):
		return &worker.JobError{Code: "model_call_failed", Retry: cost == 0}
	default:
		return err
	}
	return s.Store.Save(ctx, job, refs, vectors)
}
