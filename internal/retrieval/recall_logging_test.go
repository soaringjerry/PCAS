package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

type fakeDirectProviders struct {
	available bool
	vectors   []memory.Embedding
	err       error
}

func (f fakeDirectProviders) Available(string) bool { return f.available }
func (f fakeDirectProviders) EmbedProviderUsage(context.Context, ai.Provider, []string) ([]memory.Embedding, ai.Result, error) {
	return f.vectors, ai.Result{}, f.err
}
func (f fakeDirectProviders) Vision(context.Context, string, string, ai.Image) (ai.Result, error) {
	return ai.Result{}, nil
}
func (f fakeDirectProviders) TranscribeUsage(context.Context, io.Reader, string) (string, *int64, error) {
	return "", nil, nil
}

type fakeDirectJournal struct{}

func (fakeDirectJournal) Begin(context.Context, modelcall.DirectRequest) (modelcall.DirectCall, error) {
	return modelcall.DirectCall{InvocationID: memory.NewID(), Reservation: string(memory.NewID())}, nil
}
func (fakeDirectJournal) Finish(context.Context, modelcall.DirectRequest, modelcall.DirectCall, ai.Result, error) error {
	return nil
}

type embeddingProviderRegistry struct {
	provider ai.Provider
}

func (r embeddingProviderRegistry) EmbeddingID() string            { return r.provider.ID }
func (r embeddingProviderRegistry) Get(string) (ai.Provider, bool) { return r.provider, true }
func (r embeddingProviderRegistry) Available(string) bool          { return true }

func TestQueryEmbeddingFailureLogsConcreteReasonAndFallsBack(t *testing.T) {
	var logs bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(prior)

	provider := ai.Provider{ID: "embed-openai", Model: "text-embedding-3-small", Embedding: true}
	gw := (&modelcall.Gateway{}).WithDirectCalls(
		fakeDirectProviders{available: true, err: &ai.ProviderError{HTTPStatus: 401}},
		fakeDirectJournal{},
	)
	recaller := Recaller{
		Providers: embeddingProviderRegistry{provider: provider},
		Calls:     gw,
	}
	scope := memory.Scope{OwnerID: memory.NewID(), IsOwner: true}
	secretQuery := "private-user-query-must-never-be-logged"
	values, model, gap, err := recaller.Embed(context.Background(), scope, secretQuery)
	if err != nil || values != nil || model != "" || gap == "" {
		t.Fatalf("expected fallback gap without error, got values=%v model=%q gap=%q err=%v", values, model, gap, err)
	}
	logged := logs.String()
	if strings.Contains(logged, secretQuery) {
		t.Fatalf("private query leaked into log: %s", logged)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logged)), &entry); err != nil {
		t.Fatalf("invalid json log %q: %v", logged, err)
	}
	if entry["msg"] != "query embedding failed" || entry["provider"] != "embed-openai" || entry["model"] != "text-embedding-3-small" || entry["error_code"] != "model_call_failed" || entry["http_status"] != float64(401) {
		t.Fatalf("unexpected log entry: %+v", entry)
	}
}

func TestQueryEmbeddingEmptyResultLogsUnexpectedVectorCount(t *testing.T) {
	var logs bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(prior)

	provider := ai.Provider{ID: "embed-openai", Model: "text-embedding-3-small", Embedding: true}
	gw := (&modelcall.Gateway{}).WithDirectCalls(
		fakeDirectProviders{available: true, vectors: nil},
		fakeDirectJournal{},
	)
	recaller := Recaller{
		Providers: embeddingProviderRegistry{provider: provider},
		Calls:     gw,
	}
	scope := memory.Scope{OwnerID: memory.NewID(), IsOwner: true}
	_, _, gap, err := recaller.Embed(context.Background(), scope, "hello")
	if err != nil || gap == "" {
		t.Fatalf("expected fallback gap without error, got gap=%q err=%v", gap, err)
	}
	if !strings.Contains(logs.String(), `"msg":"query embedding returned unexpected vector count"`) {
		t.Fatalf("expected unexpected vector count warning, got %s", logs.String())
	}
}
