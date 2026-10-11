package modelcall

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

type directFixture struct {
	unavailable    bool
	calls          int
	texts          []string
	vectors        []memory.Embedding
	text           string
	err, finishErr error
	events         []string
	begun          DirectRequest
	finished       error
	usage          ai.Result
}

func (f *directFixture) Available(string) bool { return !f.unavailable }
func (f *directFixture) EmbedProviderUsage(_ context.Context, _ ai.Provider, texts []string) ([]memory.Embedding, ai.Result, error) {
	f.calls++
	f.events = append(f.events, "provider")
	f.texts = texts
	return f.vectors, ai.Result{InputTokens: 7, Cost: 0.000014}, f.err
}
func (f *directFixture) Vision(_ context.Context, _ string, instruction string, _ ai.Image) (ai.Result, error) {
	f.calls++
	f.events = append(f.events, "provider")
	f.texts = []string{instruction}
	return ai.Result{Text: f.text, InputTokens: 900, OutputTokens: 30, Cost: 0.002}, f.err
}
func (f *directFixture) TranscribeUsage(_ context.Context, audio io.Reader, _ string) (string, *int64, error) {
	f.calls++
	f.events = append(f.events, "provider")
	_, _ = io.Copy(io.Discard, audio)
	ms := int64(1200)
	return f.text, &ms, f.err
}
func (f *directFixture) Begin(_ context.Context, r DirectRequest) (DirectCall, error) {
	f.events, f.begun = append(f.events, "begin"), r
	return DirectCall{InvocationID: "call", Reservation: "reservation"}, nil
}
func (f *directFixture) Finish(_ context.Context, _ DirectRequest, _ DirectCall, usage ai.Result, callErr error) error {
	f.events = append(f.events, "finish")
	f.finished, f.usage = callErr, usage
	return f.finishErr
}

func directRequest(p ai.Provider) DirectRequest {
	p.ID, p.Model = "provider", "model"
	return DirectRequest{OwnerID: "owner", ExecutionID: "read", RootExecutionID: "read", Function: "fictitious", Stage: "stage", Provider: p, Estimate: 0.00008}
}

func directGateway(f *directFixture) *Gateway {
	return New(nil, nil, nil, nil).WithDirectCalls(f, f)
}

var queryText = []string{"query: 旧书店"}

func TestEmbeddingRecordsBeforeAndAfterTheProviderCall(t *testing.T) {
	f := &directFixture{vectors: []memory.Embedding{{Model: "vector:model", Values: []float32{0.5, -1}}}}
	vectors, err := directGateway(f).CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), queryText)
	if err != nil || !reflect.DeepEqual(vectors, f.vectors) || !reflect.DeepEqual(f.texts, queryText) {
		t.Fatal(vectors, f.texts, err)
	}
	if !reflect.DeepEqual(f.events, []string{"begin", "provider", "finish"}) || f.usage.InputTokens != 7 || f.begun.Operation != "embedding" || f.begun.Inputs != 1 || f.begun.Bytes != len(queryText[0]) {
		t.Fatal(f.events, f.usage, f.begun)
	}
}

func TestDirectCallWithoutCapabilityOrAvailabilityStartsNothing(t *testing.T) {
	f := &directFixture{}
	g := directGateway(f)
	if _, err := g.CallEmbedding(context.Background(), directRequest(ai.Provider{}), queryText); !errors.Is(err, ai.ErrUnsupportedCapability) {
		t.Fatal(err)
	}
	if _, _, _, err := g.CallTranscription(context.Background(), directRequest(ai.Provider{}), strings.NewReader("audio"), "voice.ogg", 3); !errors.Is(err, ai.ErrUnsupportedCapability) {
		t.Fatal(err)
	}
	f.unavailable = true
	if _, err := g.CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), queryText); !errors.Is(err, ErrNotAvailable) {
		t.Fatal(err)
	}
	if len(f.events) != 0 {
		t.Fatal(f.events)
	}
}

func TestDirectFailureIsRecordedAndReportsUnknownOutcome(t *testing.T) {
	f := &directFixture{err: context.Canceled}
	_, err := directGateway(f).CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), queryText)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != ErrOutcomeUnknown.Error() || failure.InvocationID != "call" || !errors.Is(f.finished, context.Canceled) {
		t.Fatal(err, f.finished)
	}
	f = &directFixture{err: errors.New("rejected")}
	_, err = directGateway(f).CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), queryText)
	if !errors.As(err, &failure) || failure.Code != "model_call_failed" || f.calls != 1 {
		t.Fatal(err, f.calls)
	}
}

func TestDirectAccountingFailureWithholdsTheOutput(t *testing.T) {
	f := &directFixture{vectors: []memory.Embedding{{Values: []float32{1}}}, finishErr: errors.New("storage")}
	vectors, err := directGateway(f).CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), queryText)
	var persistence *PersistenceError
	if vectors != nil || !errors.As(err, &persistence) {
		t.Fatal(vectors, err)
	}
}

func TestEmbeddingSubmitsLargeInputsInOrderedRequestsAndRecordsTotalUsage(t *testing.T) {
	f := &directFixture{}
	if _, err := directGateway(f).CallEmbedding(context.Background(), directRequest(ai.Provider{Embedding: true}), make([]string, 70)); err != nil {
		t.Fatal(err)
	}
	if f.calls != 3 || len(f.texts) != 6 || f.usage.InputTokens != 21 || !reflect.DeepEqual(f.events, []string{"begin", "provider", "provider", "provider", "finish"}) {
		t.Fatal(f.calls, len(f.texts), f.usage, f.events)
	}
}

func TestVisionUsesItsRegisteredInstructionsAndRecordsReportedUsage(t *testing.T) {
	f := &directFixture{text: "Fictitious receipt text."}
	request := directRequest(ai.Provider{Protocol: "openai"})
	if _, _, err := directGateway(f).CallVision(context.Background(), request, ai.Image{MediaType: "image/png", Data: []byte("image")}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("an image call without registered instructions was accepted", err)
	}
	request.Instructions = prompts.Must("vision")
	result, call, err := directGateway(f).CallVision(context.Background(), request, ai.Image{MediaType: "image/png", Data: []byte("image")})
	if err != nil || result.Text != f.text || call.Reservation != "reservation" || f.texts[0] != request.Instructions.Text() || f.begun.Operation != "vision" || f.begun.Bytes != 5 || f.usage.OutputTokens != 30 {
		t.Fatal(result, call, f.begun, f.usage, err)
	}
}

func TestTranscriptionCostIsTheAudioLengthAtThePriceForOneMinute(t *testing.T) {
	f := &directFixture{text: "虚构转写"}
	request := directRequest(ai.Provider{Transcription: true, AudioPerMinute: 0.06})
	text, usage, _, err := directGateway(f).CallTranscription(context.Background(), request, strings.NewReader("audio"), "voice.ogg", 30)
	if err != nil || text != f.text || usage.Cost != 0.03 || usage.InputTokens != 1500 || usage.OutputTokens != 4 || !usage.InputEstimated || !usage.CostEstimated || *f.usage.DurationMS != 1200 || f.begun.Operation != "transcription" {
		t.Fatalf("%q %+v %v", text, usage, err)
	}
	// A rejected request that returned no text costs nothing.
	f = &directFixture{err: errors.New("rejected")}
	if _, _, _, err := directGateway(f).CallTranscription(context.Background(), request, strings.NewReader("audio"), "voice.ogg", 30); err == nil || f.usage.Cost != 0 {
		t.Fatal(err, f.usage)
	}
	if _, _, _, err := directGateway(f).CallTranscription(context.Background(), request, strings.NewReader("audio"), "voice.ogg", 0); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("audio without a known length was accepted", err)
	}
}
