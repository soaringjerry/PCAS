package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestEmbeddingModelSwitchBackfillsAndRetainsOldVectors(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		data := []map[string]any{}
		for i := range body.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float32{1, 0, 0}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "openai-embedding", Providers: []ai.Provider{{ID: "openai-embedding", Name: "Vector", Model: "text-embedding-3-small", Protocol: "openai", BaseURL: server.URL, Embedding: true, InputPerMillion: 0.2}}}})
	src := input()
	src.Text = "以后想去河边的旧书店。"
	ref := mustIngest(t, s, scope, src).Ref
	if err := s.ProcessChunks(ctx, leaseStage(t, s, scope, ref, "source.chunk")); err != nil {
		t.Fatal(err)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding)
	 SELECT owner_id,id,version,'local-embedding:BAAI/bge-small-zh-v1.5',2,'[1,0]'::vector FROM chunks WHERE source_id=$1`, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.QueueEmbeddingBackfill(ctx, scope)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	count, err = s.QueueEmbeddingBackfill(ctx, scope)
	if err != nil || count != 0 {
		t.Fatal("duplicate backfill queued", count, err)
	}
	var stage string
	if err := s.pool.QueryRow(ctx, "SELECT stage FROM memory_jobs WHERE record_id=$1 AND stage LIKE 'memory.embed:%'", string(ref.ID)).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessEmbedding(ctx, leaseStage(t, s, scope, ref, stage)); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessEmbedding(ctx, leaseStage(t, s, scope, ref, "source.embed")); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping ingestion embedded twice")
	}
	count, err = s.QueueEmbeddingBackfill(ctx, scope)
	if err != nil || count != 0 {
		t.Fatal("completed vectors queued again", count, err)
	}
	var models int
	if err := s.pool.QueryRow(ctx, "SELECT count(DISTINCT model) FROM embeddings WHERE owner_id=$1", string(scope.OwnerID)).Scan(&models); err != nil || models != 2 {
		t.Fatal("old vectors were lost", models, err)
	}
	if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember, Budget: memory.Budget{Candidates: 10, Tokens: 2000}}); err != nil {
		t.Fatal("mixed dimensions broke recall", err)
	}
	other := scope
	other.IsOwner = false
	if _, err := s.QueueEmbeddingBackfill(ctx, other); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("non-owner queued backfill", err)
	}
}

func TestMissingEmbeddingKeyDoesNotReserveBudget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	t.Setenv("PCAS_TEST_MISSING_VECTOR_KEY", "")
	s.SetModels(&ai.Registry{Config: ai.Configuration{Embedding: "openai-embedding", Providers: []ai.Provider{{ID: "openai-embedding", Model: "text-embedding-3-small", Protocol: "openai", KeyEnv: "PCAS_TEST_MISSING_VECTOR_KEY", Embedding: true, InputPerMillion: 1}}}})
	ref := mustIngest(t, s, scope, input()).Ref
	if err := s.ProcessEmbedding(ctx, leaseStage(t, s, scope, ref, "source.embed")); !errors.Is(err, memory.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "书店", Mode: memory.Remember}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing API key reserved budget", count, err)
	}
}
