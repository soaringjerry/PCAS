package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

func queryEmbeddingModel(t *testing.T, s *Store, handler http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Model: "synthetic-vector", Protocol: "openai", BaseURL: server.URL, Embedding: true, EmbeddingQueryPrefix: "query: ", InputPerMillion: 2}}}})
	return count
}

func queryEmbeddingReply(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{0.12345679, 1, -1}}}, "usage": map[string]int{"prompt_tokens": 7}})
}

type queryCall struct {
	Execution, Root, Outcome, Accounting, Code string
	Causation                                  *string
	UsageTokens                                *int
	Reserved, Cost                             *float64
	Journal                                    string
}

func queryCalls(t *testing.T, s *Store, scope memory.Scope) []queryCall {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `SELECT c.execution_id::text,c.root_execution_id::text,c.outcome,c.accounting_state,c.error_code,c.causation_id::text,u.input_tokens,b.reserved_cost::float8,u.cost::float8,to_jsonb(c)::text
 FROM model_calls c LEFT JOIN background_usage b ON (b.owner_id,b.id)=(c.owner_id,c.reservation_id) LEFT JOIN model_usage u ON (u.owner_id,u.id)=(c.owner_id,c.usage_id) AND u.purpose='query_embedding'
 WHERE c.owner_id=$1 AND c.function_name='query_embedding' AND c.prompt_name IS NULL AND c.schema_name IS NULL AND c.required_capabilities='["embedding"]'::jsonb ORDER BY c.created_at`, string(scope.OwnerID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []queryCall
	for rows.Next() {
		var c queryCall
		if err := rows.Scan(&c.Execution, &c.Root, &c.Outcome, &c.Accounting, &c.Code, &c.Causation, &c.UsageTokens, &c.Reserved, &c.Cost, &c.Journal); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return out
}

func TestQueryEmbeddingRecordsOneSettledCallWithoutStoringTheQuery(t *testing.T) {
	s, scope := testStore(t), owner()
	mustIngest(t, s, scope, input())
	var actual []string
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Input []string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		actual = body.Input
		queryEmbeddingReply(w, r)
	})
	result, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "  旧书店  ", Mode: memory.Remember})
	if err != nil || len(result.Memories) == 0 || !reflect.DeepEqual(actual, []string{"query: 旧书店"}) || count.Load() != 1 {
		t.Fatal(result, actual, err, count.Load())
	}
	calls := queryCalls(t, s, scope)
	if len(calls) != 1 {
		t.Fatal(calls)
	}
	c := calls[0]
	if c.Outcome != "returned" || c.Accounting != "settled" || c.Execution != c.Root || c.Causation != nil || c.UsageTokens == nil || *c.UsageTokens != 7 || c.Cost == nil || c.Reserved == nil || *c.Reserved != *c.Cost || *c.Cost != 0.000014 {
		t.Fatalf("%+v", c)
	}
	for _, private := range []string{"旧书店", "0.12345679"} {
		if strings.Contains(c.Journal, private) {
			t.Fatal("journal stored private query content")
		}
	}
	var saved int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM background_model_results WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&saved); err != nil || saved != 0 {
		t.Fatal("query embedding saved a result body", saved, err)
	}
}

func TestQueryEmbeddingInsideAnExecutionKeepsItsRootAndCause(t *testing.T) {
	s, scope := testStore(t), owner()
	mustIngest(t, s, scope, input())
	queryEmbeddingModel(t, s, queryEmbeddingReply)
	parent := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), RootExecutionID: memory.NewID()}
	ctx := context.WithValue(context.Background(), interactiveExecutionKey{}, parent)
	for range 2 {
		if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember}); err != nil {
			t.Fatal(err)
		}
	}
	calls := queryCalls(t, s, scope)
	if len(calls) != 2 || calls[0].Execution == calls[1].Execution {
		t.Fatal(calls)
	}
	for _, c := range calls {
		if c.Root != string(parent.RootExecutionID) || c.Causation == nil || *c.Causation != string(parent.ExecutionID) || c.Outcome != "returned" {
			t.Fatalf("%+v", c)
		}
	}
}

func TestQueryEmbeddingWithoutAUsableProviderFallsBackWithoutACallOrCharge(t *testing.T) {
	for _, kind := range []string{"missing_key", "unsupported", "unconfigured"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			mustIngest(t, s, scope, input())
			count := queryEmbeddingModel(t, s, queryEmbeddingReply)
			switch kind {
			case "missing_key":
				t.Setenv("PCAS_QUERY_TEST_MISSING_KEY", "")
				s.models.Config.Providers[0].KeyEnv = "PCAS_QUERY_TEST_MISSING_KEY"
			case "unsupported":
				s.models.Config.Providers[0].Embedding = false
			default:
				s.SetModels(nil)
			}
			result, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember})
			if err != nil || result.Coverage.Complete || len(result.Coverage.Gaps) == 0 || len(result.Memories) == 0 || count.Load() != 0 {
				t.Fatal(result, err, count.Load())
			}
			var reservations int
			if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM background_usage WHERE owner_id=$1 AND job_id IS NULL`, string(scope.OwnerID)).Scan(&reservations); err != nil || reservations != 0 || len(queryCalls(t, s, scope)) != 0 {
				t.Fatal("unusable provider was charged or recorded as called", reservations, err)
			}
		})
	}
}

func TestQueryEmbeddingRejectedCallIsRecordedAndRecallUsesTextSearch(t *testing.T) {
	s, scope := testStore(t), owner()
	mustIngest(t, s, scope, input())
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "rejected", http.StatusBadRequest) })
	result, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember})
	if err != nil || result.Coverage.Complete || len(result.Memories) == 0 || count.Load() != 1 {
		t.Fatal(result, err, count.Load())
	}
	calls := queryCalls(t, s, scope)
	if len(calls) != 1 || calls[0].Outcome != "failed" || calls[0].Code != "model_call_failed" || calls[0].Accounting != "settled" {
		t.Fatalf("%+v", calls)
	}
}

func TestQueryEmbeddingCanceledCallStaysUnknownAndKeepsItsReservation(t *testing.T) {
	s, scope := testStore(t), owner()
	mustIngest(t, s, scope, input())
	started := make(chan struct{})
	release := make(chan struct{})
	count := queryEmbeddingModel(t, s, func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember}); err == nil {
		t.Fatal("canceled read returned a result")
	}
	calls := queryCalls(t, s, scope)
	if len(calls) != 1 || count.Load() != 1 {
		t.Fatal(calls, count.Load())
	}
	c := calls[0]
	if c.Outcome != "unknown" || c.Code != modelcall.ErrOutcomeUnknown.Error() || c.Accounting != "held" || c.UsageTokens != nil || c.Reserved == nil || *c.Reserved <= 0 {
		t.Fatalf("%+v", c)
	}
}

func TestQueryEmbeddingInterruptedProcessLeavesAnUnknownHeldCall(t *testing.T) {
	s, scope := testStore(t), owner()
	mustIngest(t, s, scope, input())
	queryEmbeddingModel(t, s, queryEmbeddingReply)
	provider, _ := s.models.Get("vector")
	request := modelcall.EmbeddingRequest{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), Function: "query_embedding", Stage: "query", Provider: provider, Texts: []string{"query: 旧书店"}, Estimate: 0.00005}
	request.RootExecutionID = request.ExecutionID
	adapter := embeddingCalls{store: s}
	for range 2 {
		request.ExecutionID = memory.NewID()
		if _, err := adapter.Begin(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	// One call is older than every provider timeout. The other can still be running.
	if _, err := s.pool.Exec(context.Background(), `UPDATE model_calls SET created_at=created_at-interval '11 minutes',started_at=started_at-interval '11 minutes' WHERE owner_id=$1 AND execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.recoverInteractiveCallsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	calls := queryCalls(t, s, scope)
	if len(calls) != 2 || calls[0].Outcome != "unknown" || calls[0].Accounting != "held" || *calls[0].Reserved != 0.00005 || calls[1].Outcome != "started" || calls[1].Accounting != "reserved" {
		t.Fatalf("%+v", calls)
	}
}
