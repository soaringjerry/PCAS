package modelcall

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// The provider request accepts at most this many inputs. A call with more
// inputs submits several requests and records their total usage once.
const embeddingBatch = 32

// DirectRequest identifies one call that is not text generation: an embedding,
// an image interpretation, or a transcription. Its input is never stored.
// The journal keeps the provider, the sizes, the outcome, and the accounting.
type DirectRequest struct {
	OwnerID         memory.ID
	ExecutionID     memory.ID
	RootExecutionID memory.ID
	CausationID     memory.ID
	Function        string
	Stage           string
	Provider        ai.Provider
	Estimate        float64
	// Instructions is the registered prompt of an image interpretation.
	// Embeddings and transcriptions have none.
	Instructions prompts.Definition
	// Refs name the records that the input belongs to. A query has none.
	Refs []memory.Ref
	// Job is the leased background job that pays for this call. A read has none.
	// The gateway carries it to the journal and does not apply queue rules.
	Job *worker.Job
	// UsageJob names a job in the usage record when that job's queue budget
	// rules do not apply to this call.
	UsageJob memory.ID
	// The gateway sets the operation and the input sizes for the journal.
	Operation string
	Inputs    int
	Bytes     int
}

// DirectCall links the journal record to its budget reservation.
type DirectCall struct {
	InvocationID memory.ID
	Reservation  string
}

// DirectProviders are the provider entries of the direct operations.
type DirectProviders interface {
	Available(string) bool
	EmbedProviderUsage(context.Context, ai.Provider, []string) ([]memory.Embedding, ai.Result, error)
	Vision(context.Context, string, string, ai.Image) (ai.Result, error)
	TranscribeUsage(context.Context, io.Reader, string) (string, *int64, error)
}

// DirectJournal records an invocation and its accounting.
// Begin reserves the estimate and records the started call in one transaction.
// Finish records usage, settles the reservation, and stores the outcome in one
// transaction that does not depend on the caller's context.
type DirectJournal interface {
	Begin(context.Context, DirectRequest) (DirectCall, error)
	Finish(context.Context, DirectRequest, DirectCall, ai.Result, error) error
}

// WithDirectCalls adds the embedding, image, and transcription entries.
func (g *Gateway) WithDirectCalls(providers DirectProviders, journal DirectJournal) *Gateway {
	g.direct, g.directJournal = providers, journal
	return g
}

// recorded runs one direct call. It keeps no result: a caller that loses the
// output submits a new call. A failed accounting write returns
// PersistenceError, and the caller must not use the output as if the call had
// been recorded.
func (g *Gateway) recorded(ctx context.Context, request DirectRequest, capable bool, invoke func() (ai.Result, error)) (DirectCall, error) {
	if request.OwnerID == "" || request.ExecutionID == "" || request.RootExecutionID == "" || request.Function == "" || request.Stage == "" || request.Provider.ID == "" || request.Provider.Model == "" || request.Inputs == 0 || request.Estimate < 0 || math.IsNaN(request.Estimate) || math.IsInf(request.Estimate, 0) {
		return DirectCall{}, memory.ErrInvalid
	}
	if g.direct == nil || g.directJournal == nil {
		return DirectCall{}, ErrNotConfigured
	}
	if !capable {
		return DirectCall{}, &ai.CapabilityError{Capability: request.Operation}
	}
	if !g.direct.Available(request.Provider.ID) {
		return DirectCall{}, ErrNotAvailable
	}
	call, err := g.directJournal.Begin(ctx, request)
	if err != nil {
		return DirectCall{}, err
	}
	usage, callErr := invoke()
	if err := g.directJournal.Finish(ctx, request, call, usage, callErr); err != nil {
		return call, &PersistenceError{Err: errors.Join(err, callErr)}
	}
	if callErr != nil {
		code := "model_call_failed"
		if errors.Is(callErr, memory.ErrUnavailable) {
			code = ErrNotAvailable.Error()
		}
		if OutcomeUnknown(callErr) {
			code = ErrOutcomeUnknown.Error()
		}
		return call, &Failure{Code: code, InvocationID: call.InvocationID, Details: failureDetails(callErr), Reservation: call.Reservation}
	}
	return call, nil
}

// CallEmbedding returns one vector for each text, in order.
func (g *Gateway) CallEmbedding(ctx context.Context, request DirectRequest, texts []string) ([]memory.Embedding, error) {
	request.Operation, request.Inputs, request.Bytes = "embedding", len(texts), 0
	for _, text := range texts {
		request.Bytes += len(text)
	}
	var vectors []memory.Embedding
	_, err := g.recorded(ctx, request, request.Provider.Embedding, func() (ai.Result, error) {
		var usage ai.Result
		var callErr error
		for start := 0; start < len(texts) && callErr == nil; start += embeddingBatch {
			batch, batchUsage, err := g.direct.EmbedProviderUsage(ctx, request.Provider, texts[start:min(start+embeddingBatch, len(texts))])
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
		return usage, callErr
	})
	if err != nil {
		return nil, err
	}
	return vectors, nil
}

// CallVision interprets one image with the registered instructions.
// The result carries the text and the usage that the journal recorded.
func (g *Gateway) CallVision(ctx context.Context, request DirectRequest, image ai.Image) (ai.Result, DirectCall, error) {
	request.Operation, request.Inputs, request.Bytes = "vision", 1, len(image.Data)
	if request.Instructions.Name() == "" {
		return ai.Result{}, DirectCall{}, memory.ErrInvalid
	}
	var result ai.Result
	call, err := g.recorded(ctx, request, request.Provider.SupportsVision(), func() (ai.Result, error) {
		var callErr error
		result, callErr = g.direct.Vision(ctx, request.Provider.ID, request.Instructions.Text(), image)
		return result, callErr
	})
	return result, call, err
}

// CallTranscription transcribes audio of a known length with the configured
// transcription provider. The provider reports text and no billing, so the
// recorded cost is the audio length at the provider's price for one minute.
// Token counts are estimates. A failed call that returned no text costs nothing.
func (g *Gateway) CallTranscription(ctx context.Context, request DirectRequest, audio io.Reader, name string, seconds float64) (string, ai.Result, DirectCall, error) {
	request.Operation, request.Inputs, request.Bytes = "transcription", 1, 0
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return "", ai.Result{}, DirectCall{}, memory.ErrInvalid
	}
	var text string
	var usage ai.Result
	call, err := g.recorded(ctx, request, request.Provider.Transcription, func() (ai.Result, error) {
		var callErr error
		text, usage.DurationMS, callErr = g.direct.TranscribeUsage(ctx, audio, name)
		usage.InputTokens, usage.OutputTokens = int(math.Ceil(seconds*50)), len([]rune(text))
		usage.InputEstimated, usage.OutputEstimated = true, true
		usage.Cost, usage.CostEstimated = seconds/60*request.Provider.AudioPerMinute, request.Provider.CostMode != "free"
		if callErr != nil && strings.TrimSpace(text) == "" {
			usage.Cost = 0
		}
		return usage, callErr
	})
	return text, usage, call, err
}
