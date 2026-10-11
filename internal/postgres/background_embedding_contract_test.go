package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// A source with more than 32 chunks needs several provider requests.
func largeEmbeddingJob(t *testing.T, s *Store, scope memory.Scope, price float64) (worker.Job, int, *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Texts []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		data := make([]map[string]any, len(body.Texts))
		for i := range body.Texts {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, 0, 0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 10 * len(body.Texts)}})
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Protocol: "openai", BaseURL: server.URL, Model: "synthetic", Embedding: true, InputPerMillion: price}}}})
	in := input()
	in.Text = strings.Repeat("a", 35000)
	ref := mustIngest(t, s, scope, in).Ref
	if err := s.ProcessChunks(ctx, leaseStage(t, s, scope, ref, "source.chunk")); err != nil {
		t.Fatal(err)
	}
	var chunks int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM chunks WHERE owner_id=$1 AND source_id=$2 AND source_version=$3", string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&chunks); err != nil || chunks <= 32 {
		t.Fatal("fixture must need more than one provider request", chunks, err)
	}
	return leaseStage(t, s, scope, ref, "source.embed"), chunks, calls
}

func rejectEmbeddingWrites(t *testing.T, s *Store) func() {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_embedding_fixture_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'embedding_storage_fixture'; END $$;
CREATE TRIGGER reject_embedding_fixture_write BEFORE INSERT OR UPDATE ON embeddings FOR EACH ROW EXECUTE FUNCTION reject_embedding_fixture_write()`); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_embedding_fixture_write ON embeddings"); err != nil {
			t.Fatal(err)
		}
	}
}

// The queue gives a retried job a new lease and a higher attempt number.
func nextEmbeddingAttempt(t *testing.T, s *Store, job worker.Job) worker.Job {
	t.Helper()
	job.Attempts, job.LeaseToken = job.Attempts+1, memory.NewID()
	tag, err := s.pool.Exec(context.Background(), `UPDATE memory_jobs SET state='leased',attempts=$2,lease_token=$3,lease_until=now()+interval '5 minutes' WHERE id=$1`, string(job.ID), job.Attempts, string(job.LeaseToken))
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal(tag, err)
	}
	return job
}

func TestBackgroundEmbeddingRecordsOneSettledCallForAJob(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	job, chunks, calls := largeEmbeddingJob(t, s, scope, 2)
	if err := s.ProcessEmbedding(ctx, job); err != nil {
		t.Fatal(err)
	}
	requests := int32((chunks + 31) / 32)
	var vectors, journal, usage, tokens int
	var settled bool
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM embeddings WHERE owner_id=$1),
 (SELECT count(*) FROM model_calls WHERE owner_id=$1 AND function_name='embedding' AND outcome='returned' AND accounting_state='settled' AND root_execution_id=$2::uuid AND causation_id=$2::uuid AND jsonb_array_length(input_manifest->'memoryRefs')=$3),
 (SELECT count(*) FROM model_usage WHERE owner_id=$1 AND job_id::text=$4 AND purpose='embedding'),
 (SELECT coalesce(sum(input_tokens),0) FROM model_usage WHERE owner_id=$1 AND job_id::text=$4),
 (SELECT bool_and(b.reserved_cost=u.cost) FROM model_usage u JOIN background_usage b ON (b.owner_id,b.id)=(u.owner_id,u.id) WHERE u.owner_id=$1)`,
		string(scope.OwnerID), string(job.ID), chunks, string(job.ID)).Scan(&vectors, &journal, &usage, &tokens, &settled); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != requests || vectors != chunks || journal != 1 || usage != 1 || tokens != 10*chunks || !settled {
		t.Fatal(calls.Load(), requests, vectors, chunks, journal, usage, tokens, settled)
	}
	var journalText string
	if err := s.pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM model_calls c WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&journalText); err != nil || strings.Contains(journalText, "aaaa") {
		t.Fatal("journal stored source text", err)
	}
}

func TestPaidEmbeddingJobIsNotSubmittedAgainAfterItsVectorWriteFails(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	job, _, calls := largeEmbeddingJob(t, s, scope, 2)
	restore := rejectEmbeddingWrites(t, s)
	if err := s.ProcessEmbedding(ctx, job); err == nil || !strings.Contains(err.Error(), "embedding_storage_fixture") {
		t.Fatal("expected the vector write to fail after the provider returned", err)
	}
	first := calls.Load()
	restore()
	err := s.ProcessEmbedding(ctx, nextEmbeddingAttempt(t, s, job))
	var stopped *worker.JobError
	if !errors.As(err, &stopped) || stopped.Code != "model_call_failed" || stopped.Retry || calls.Load() != first {
		t.Fatal("a paid job was submitted again", err, first, calls.Load())
	}
}

func TestFreeEmbeddingJobRetriesAfterItsVectorWriteFails(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	job, chunks, calls := largeEmbeddingJob(t, s, scope, 0)
	restore := rejectEmbeddingWrites(t, s)
	if err := s.ProcessEmbedding(ctx, job); err == nil {
		t.Fatal("expected the vector write to fail")
	}
	first := calls.Load()
	restore()
	if err := s.ProcessEmbedding(ctx, nextEmbeddingAttempt(t, s, job)); err != nil {
		t.Fatal(err)
	}
	var vectors int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM embeddings WHERE owner_id=$1", string(scope.OwnerID)).Scan(&vectors); err != nil || vectors != chunks || calls.Load() != 2*first {
		t.Fatal(vectors, chunks, first, calls.Load(), err)
	}
}
