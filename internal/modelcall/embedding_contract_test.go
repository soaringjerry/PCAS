package modelcall

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

type embeddingFixture struct {
	*generationFixture
	texts   []string
	vectors []memory.Embedding
	err     error
}

func (f *embeddingFixture) EmbedProviderUsage(_ context.Context, p ai.Provider, texts []string) ([]memory.Embedding, ai.Result, error) {
	f.calls++
	f.called = p
	f.texts = append([]string(nil), texts...)
	return f.vectors, ai.Result{InputTokens: 7, Cost: 0.000014}, f.err
}

func embeddingRequest() Request {
	p := ai.Provider{ID: "vector", Model: "original-model", Embedding: true, InputPerMillion: 2}
	estimate := 0.00008
	return Request{OwnerID: "owner", ExecutionID: "read", RootExecutionID: "read", Function: "query_embedding", Stage: "query", ProviderID: p.ID, Operation: "embedding", ProviderSnapshot: &p, ReservationEstimate: &estimate, Prompt: json.RawMessage(`["Original prefix:原始查询"]`)}
}

func TestEmbeddingGatewayPreservesInputsVectorsAndSelectedProvider(t *testing.T) {
	f := &embeddingFixture{generationFixture: &generationFixture{provider: ai.Provider{ID: "vector", Model: "changed-model"}}, vectors: []memory.Embedding{{Model: "vector:original-model", Values: []float32{0.12345679, -1, 0}}}}
	r := embeddingRequest()
	f.afterReserve = func() { f.provider.Model = "changed-again"; r.ProviderSnapshot.Model = "changed-after-reservation" }
	paid, err := New(f, f, f, f).Call(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	var restored []memory.Embedding
	if err := json.Unmarshal([]byte(paid.Output), &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, f.vectors) || !reflect.DeepEqual(f.texts, []string{"Original prefix:原始查询"}) || f.called.Model != "original-model" || paid.Model != "original-model" {
		t.Fatal("embedding input, selected model, or vector changed", paid, f.called, restored)
	}
	if f.estimate != 0.00008 || f.prepared.Instructions.Name() != "" || f.prepared.Schema.Name() != "" || !reflect.DeepEqual(f.prepared.RequiredCapabilities(), []string{"embedding"}) || f.mode.Schema != nil {
		t.Fatal("embedding received text-generation metadata or a different reservation", f.prepared, f.estimate)
	}
}

func TestEmbeddingPersistenceRecoveryDoesNotRepeatProvider(t *testing.T) {
	for _, stage := range []string{"save", "record", "settle"} {
		t.Run(stage, func(t *testing.T) {
			f := &embeddingFixture{generationFixture: &generationFixture{provider: *embeddingRequest().ProviderSnapshot, failAt: stage}, vectors: []memory.Embedding{{Model: "vector:original-model", Values: []float32{1, 2}}}}
			r := embeddingRequest()
			_, err := New(f, f, f, f).Call(context.Background(), r)
			var persistence *PersistenceError
			if !errors.As(err, &persistence) {
				t.Fatal("persistence failure hidden", err)
			}
			paid, err := New(nil, f, f, f).Call(context.Background(), r)
			if err != nil || paid == nil || f.calls != 1 || f.reservations != 1 {
				t.Fatal("recovery repeated an embedding", paid, err, f.calls, f.reservations)
			}
		})
	}
}

func TestEmbeddingCapabilityFailureRecordsBeforeReservation(t *testing.T) {
	f := &generationFixture{provider: *embeddingRequest().ProviderSnapshot}
	_, err := New(f, f, f, f).Call(context.Background(), embeddingRequest())
	if !errors.Is(err, ai.ErrUnsupportedCapability) || f.failure == nil || f.reservations != 0 || f.calls != 0 || f.prepared.Operation != "embedding" {
		t.Fatal(err, f)
	}
}

func TestEmbeddingDoesNotAcceptGenerationInstructionsOrSchema(t *testing.T) {
	for _, change := range []func(*Request){func(r *Request) { r.Instructions = prompts.Must("organize") }, func(r *Request) { r.Schema = prompts.MustSchema("secretary-output") }, func(r *Request) { r.Search = true }} {
		r := embeddingRequest()
		change(&r)
		f := &generationFixture{}
		if _, err := New(f, f, f, f).Call(context.Background(), r); !errors.Is(err, memory.ErrInvalid) || f.prepared.ExecutionID != "" || f.reservations != 0 {
			t.Fatal("invalid embedding started work", err, f)
		}
	}
}

func TestEmbeddingInterruptionKeepsUnknownOutcome(t *testing.T) {
	f := &embeddingFixture{generationFixture: &generationFixture{provider: *embeddingRequest().ProviderSnapshot}, err: context.DeadlineExceeded}
	_, err := New(f, f, f, f).Call(context.Background(), embeddingRequest())
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) || f.calls != 1 || f.records != 1 || f.settlements != 1 {
		t.Fatal(err, f)
	}
}
