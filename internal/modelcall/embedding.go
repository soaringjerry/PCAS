package modelcall

import (
	"context"
	"errors"
	"math"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// The provider request accepts at most this many inputs. A call with more
// inputs submits several requests and records their total usage once.
const embeddingBatch = 32

// EmbeddingRequest identifies one embedding invocation.
// It has no instructions or output schema, and its texts are never stored.
type EmbeddingRequest struct {
	OwnerID         memory.ID
	ExecutionID     memory.ID
	RootExecutionID memory.ID
	CausationID     memory.ID
	Function        string
	Stage           string
	Provider        ai.Provider
	Texts           []string
	Estimate        float64
	// Refs name the records that the texts belong to. A query has none.
	Refs []memory.Ref
	// Job is the leased background job that pays for this call. A read has none.
	// The gateway carries it to the journal and does not apply queue rules.
	Job *worker.Job
}

// EmbeddingCall links the journal record to its budget reservation.
type EmbeddingCall struct {
	InvocationID memory.ID
	Reservation  string
}

type Embedder interface {
	Available(string) bool
	EmbedProviderUsage(context.Context, ai.Provider, []string) ([]memory.Embedding, ai.Result, error)
}

// EmbeddingJournal records an invocation and its accounting.
// Begin reserves the estimate and records the started call in one transaction.
// Finish records usage, settles the reservation, and stores the outcome in one
// transaction that does not depend on the caller's context.
type EmbeddingJournal interface {
	Begin(context.Context, EmbeddingRequest) (EmbeddingCall, error)
	Finish(context.Context, EmbeddingRequest, EmbeddingCall, ai.Result, error) error
}

// WithEmbeddings adds the embedding entry to a gateway.
func (g *Gateway) WithEmbeddings(embedder Embedder, journal EmbeddingJournal) *Gateway {
	g.embedder, g.embeddings = embedder, journal
	return g
}

// CallEmbedding keeps no result. A caller that loses the vectors submits a new call.
// A failed accounting write returns PersistenceError, and the caller must not
// use the vectors as if the call had been recorded.
func (g *Gateway) CallEmbedding(ctx context.Context, request EmbeddingRequest) ([]memory.Embedding, error) {
	if request.OwnerID == "" || request.ExecutionID == "" || request.RootExecutionID == "" || request.Function == "" || request.Stage == "" || request.Provider.ID == "" || request.Provider.Model == "" || len(request.Texts) == 0 || request.Estimate < 0 || math.IsNaN(request.Estimate) || math.IsInf(request.Estimate, 0) {
		return nil, memory.ErrInvalid
	}
	if g.embedder == nil || g.embeddings == nil {
		return nil, ErrNotConfigured
	}
	if !request.Provider.Embedding {
		return nil, &ai.CapabilityError{Capability: "embedding"}
	}
	if !g.embedder.Available(request.Provider.ID) {
		return nil, ErrNotAvailable
	}
	call, err := g.embeddings.Begin(ctx, request)
	if err != nil {
		return nil, err
	}
	var vectors []memory.Embedding
	var usage ai.Result
	var callErr error
	for start := 0; start < len(request.Texts) && callErr == nil; start += embeddingBatch {
		batch, batchUsage, err := g.embedder.EmbedProviderUsage(ctx, request.Provider, request.Texts[start:min(start+embeddingBatch, len(request.Texts))])
		vectors, callErr = append(vectors, batch...), err
		usage.InputTokens += batchUsage.InputTokens
		usage.Cost += batchUsage.Cost
		usage.InputEstimated = usage.InputEstimated || batchUsage.InputEstimated
		usage.CostEstimated = usage.CostEstimated || batchUsage.CostEstimated
		if batchUsage.DurationMS != nil {
			total := *batchUsage.DurationMS
			if usage.DurationMS != nil {
				total += *usage.DurationMS
			}
			usage.DurationMS = &total
		}
	}
	if err := g.embeddings.Finish(ctx, request, call, usage, callErr); err != nil {
		return nil, &PersistenceError{Err: errors.Join(err, callErr)}
	}
	if callErr != nil {
		code := "model_call_failed"
		if errors.Is(callErr, memory.ErrUnavailable) {
			code = ErrNotAvailable.Error()
		}
		if OutcomeUnknown(callErr) {
			code = ErrOutcomeUnknown.Error()
		}
		return nil, &Failure{Code: code, InvocationID: call.InvocationID, Details: failureDetails(callErr), Reservation: call.Reservation}
	}
	return vectors, nil
}
