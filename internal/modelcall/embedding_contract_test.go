package modelcall

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type embeddingFixture struct {
	unavailable       bool
	calls             int
	texts             []string
	vectors           []memory.Embedding
	err, finishErr    error
	events            []string
	finished          error
	finishedWithUsage ai.Result
}

func (f *embeddingFixture) Available(string) bool { return !f.unavailable }
func (f *embeddingFixture) EmbedProviderUsage(_ context.Context, _ ai.Provider, texts []string) ([]memory.Embedding, ai.Result, error) {
	f.calls++
	f.events = append(f.events, "provider")
	f.texts = texts
	return f.vectors, ai.Result{InputTokens: 7, Cost: 0.000014}, f.err
}
func (f *embeddingFixture) Begin(context.Context, EmbeddingRequest) (EmbeddingCall, error) {
	f.events = append(f.events, "begin")
	return EmbeddingCall{InvocationID: "call", Reservation: "reservation"}, nil
}
func (f *embeddingFixture) Finish(_ context.Context, _ EmbeddingRequest, _ EmbeddingCall, usage ai.Result, callErr error) error {
	f.events = append(f.events, "finish")
	f.finished, f.finishedWithUsage = callErr, usage
	return f.finishErr
}

func embeddingRequest() EmbeddingRequest {
	return EmbeddingRequest{OwnerID: "owner", ExecutionID: "read", RootExecutionID: "read", Function: "query_embedding", Stage: "query", Provider: ai.Provider{ID: "vector", Model: "model", Embedding: true}, Texts: []string{"query: 旧书店"}, Estimate: 0.00008}
}

func embeddingGateway(f *embeddingFixture) *Gateway {
	return New(nil, nil, nil, nil).WithEmbeddings(f, f)
}

func TestEmbeddingRecordsBeforeAndAfterTheProviderCall(t *testing.T) {
	f := &embeddingFixture{vectors: []memory.Embedding{{Model: "vector:model", Values: []float32{0.5, -1}}}}
	vectors, err := embeddingGateway(f).CallEmbedding(context.Background(), embeddingRequest())
	if err != nil || !reflect.DeepEqual(vectors, f.vectors) || !reflect.DeepEqual(f.texts, []string{"query: 旧书店"}) {
		t.Fatal(vectors, f.texts, err)
	}
	if !reflect.DeepEqual(f.events, []string{"begin", "provider", "finish"}) || f.finishedWithUsage.InputTokens != 7 {
		t.Fatal(f.events, f.finishedWithUsage)
	}
}

func TestEmbeddingWithoutCapabilityOrAvailabilityStartsNothing(t *testing.T) {
	f := &embeddingFixture{}
	r := embeddingRequest()
	r.Provider.Embedding = false
	if _, err := embeddingGateway(f).CallEmbedding(context.Background(), r); !errors.Is(err, ai.ErrUnsupportedCapability) {
		t.Fatal(err)
	}
	f.unavailable = true
	if _, err := embeddingGateway(f).CallEmbedding(context.Background(), embeddingRequest()); !errors.Is(err, ErrNotAvailable) {
		t.Fatal(err)
	}
	if len(f.events) != 0 {
		t.Fatal(f.events)
	}
}

func TestEmbeddingFailureIsRecordedAndReportsUnknownOutcome(t *testing.T) {
	f := &embeddingFixture{err: context.Canceled}
	_, err := embeddingGateway(f).CallEmbedding(context.Background(), embeddingRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != ErrOutcomeUnknown.Error() || failure.InvocationID != "call" || !errors.Is(f.finished, context.Canceled) {
		t.Fatal(err, f.finished)
	}
	f = &embeddingFixture{err: errors.New("rejected")}
	_, err = embeddingGateway(f).CallEmbedding(context.Background(), embeddingRequest())
	if !errors.As(err, &failure) || failure.Code != "model_call_failed" || f.calls != 1 {
		t.Fatal(err, f.calls)
	}
}

func TestEmbeddingAccountingFailureWithholdsTheVectors(t *testing.T) {
	f := &embeddingFixture{vectors: []memory.Embedding{{Values: []float32{1}}}, finishErr: errors.New("storage")}
	vectors, err := embeddingGateway(f).CallEmbedding(context.Background(), embeddingRequest())
	var persistence *PersistenceError
	if vectors != nil || !errors.As(err, &persistence) {
		t.Fatal(vectors, err)
	}
}

func TestEmbeddingSubmitsLargeInputsInOrderedRequestsAndRecordsTotalUsage(t *testing.T) {
	f := &embeddingFixture{}
	r := embeddingRequest()
	r.Texts = make([]string, 70)
	if _, err := embeddingGateway(f).CallEmbedding(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if f.calls != 3 || len(f.texts) != 6 || f.finishedWithUsage.InputTokens != 21 || !reflect.DeepEqual(f.events, []string{"begin", "provider", "provider", "provider", "finish"}) {
		t.Fatal(f.calls, len(f.texts), f.finishedWithUsage, f.events)
	}
}
