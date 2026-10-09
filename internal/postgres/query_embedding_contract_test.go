package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func queryEmbeddingReply(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{0.12345679, 1, -1}}}, "usage": map[string]int{"prompt_tokens": 7}})
}

func queryCallRow(t *testing.T, s *Store, scope memory.Scope) interactiveCallRow {
	t.Helper()
	var row interactiveCallRow
	if err := s.pool.QueryRow(context.Background(), `SELECT to_jsonb(c) FROM model_calls c WHERE owner_id=$1 AND function_name='query_embedding' ORDER BY created_at DESC LIMIT 1`, string(scope.OwnerID)).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func queryReadRequest(t *testing.T, s *Store, scope memory.Scope) (modelcall.Request, func() error) {
	t.Helper()
	r, release, err := (interactiveCalls{store: s}).beginRecall(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.models.Get(s.models.EmbeddingID())
	estimate := 0.00006
	r.Operation, r.Stage, r.ProviderID = "embedding", "query:synthetic", p.ID
	r.ProviderSnapshot, r.ReservationEstimate, r.ContextBuilderVersion = &p, &estimate, "recall-query-v1"
	r.Prompt = asJSON([]string{"query: synthetic"})
	policy := r.Policy.(interactiveCallPolicy)
	policy.Usage = modelUsage{Purpose: "query_embedding"}
	r.Policy = policy
	return r, release
}

func TestQueryEmbeddingKeepsCallerInputBillingAndReadOnlyData(t *testing.T) {
	s, scope := testStore(t), owner()
	source := mustIngest(t, s, scope, input()).Ref
	// A granted principal can recall without being a workspace team agent.
	scope.PrincipalID, scope.IsOwner = "granted-reader", false
	if _, err := s.pool.Exec(context.Background(), `INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)`, string(scope.OwnerID), string(source.ID), scope.PrincipalID); err != nil {
		t.Fatal(err)
	}
	var actual []string
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Input []string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		actual = body.Input
		queryEmbeddingReply(w)
	})
	result, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "  旧书店  ", Mode: memory.Remember})
	if err != nil || len(result.Memories) == 0 || !reflect.DeepEqual(actual, []string{"query: 旧书店"}) || count.Load() != 1 {
		t.Fatal(result, actual, err, count.Load())
	}
	row := queryCallRow(t, s, scope)
	if row.Manifest.Kind != "recall" || row.ExecutionID != row.RootExecutionID || row.Outcome != "returned" || row.Accounting != "settled" || row.Receipt.Available || row.Receipt.Billing == nil || row.Receipt.Billing.Cost != 0.000014 || row.Receipt.Billing.Output != "" {
		t.Fatal(row)
	}
	var valid bool
	if err := s.pool.QueryRow(context.Background(), `SELECT prompt_name IS NULL AND instruction_hash IS NULL AND schema_name IS NULL AND required_capabilities='["embedding"]'::jsonb AND input_manifest->'callerScope'->>'principalId'=$3 AND actual_mode->>'applicationOutcome'='used' AND actual_mode->>'inputPayload'='discarded' AND usage_id=reservation_id AND EXISTS(SELECT 1 FROM model_usage u JOIN background_usage b ON b.id=u.id WHERE u.owner_id=c.owner_id AND u.id=c.usage_id AND u.purpose='query_embedding' AND u.input_tokens=7 AND b.reserved_cost=u.cost) AND NOT EXISTS(SELECT 1 FROM background_model_results b WHERE b.owner_id=c.owner_id AND b.job_id=c.id) FROM model_calls c WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(row.ID), scope.PrincipalID).Scan(&valid); err != nil || !valid {
		t.Fatal("input metadata, settlement, or private cleanup mismatch", valid, err)
	}
	if len(s.secretarySlots) != 0 {
		t.Fatal("read retained a foreground slot")
	}
	if err := s.pool.QueryRow(context.Background(), `SELECT NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory')`, row.Mode.ReadPID).Scan(&valid); err != nil || !valid {
		t.Fatal("read retained its session lock", err)
	}
}

func TestQueryEmbeddingNestedStageReusesVectorWithoutGenerationMetadata(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	parent := admittedInteractiveRequest(t, s, scope, "vector")
	ctx := context.WithValue(context.Background(), interactiveExecutionKey{}, parent)
	for range 2 {
		if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember}); err != nil {
			t.Fatal(err)
		}
	}
	row := queryCallRow(t, s, scope)
	if row.ExecutionID != parent.ExecutionID || row.Manifest.Kind != "secretary" || row.Manifest.BudgetOwner != "" || count.Load() != 1 || row.Mode.ReadPID != 0 {
		t.Fatal("nested read changed its execution or repeated the call", row, count.Load())
	}
	var saved string
	if err := s.pool.QueryRow(ctx, `SELECT output FROM background_model_results WHERE job_id=$1`, string(row.ID)).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	var vectors []memory.Embedding
	if err := json.Unmarshal([]byte(saved), &vectors); err != nil || !reflect.DeepEqual(vectors, []memory.Embedding{{Model: "vector:synthetic-vector", Values: []float32{0.12345679, 1, -1}}}) {
		t.Fatal("saved vector changed", vectors, err)
	}
	// A distinct team scope gets its own bound query stage.
	scope.Team = true
	result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
	if err != nil || count.Load() != 2 || len(result.Coverage.Gaps) != 0 {
		t.Fatal("distinct scope was not separately bound", result, err, count.Load())
	}
}

func TestQueryEmbeddingUnavailableCapabilityHasVisibleFallbackWithoutReservation(t *testing.T) {
	for _, kind := range []string{"missing_key", "unsupported", "unconfigured"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
			if kind == "missing_key" {
				t.Setenv("PCAS_QUERY_TEST_MISSING_KEY", "")
				s.models.Config.Providers[0].KeyEnv = "PCAS_QUERY_TEST_MISSING_KEY"
			} else if kind == "unsupported" {
				s.models.Config.Providers[0].Embedding = false
			} else {
				s.SetModels(nil)
			}
			result, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
			if err != nil || result.Coverage.Complete || len(result.Coverage.Gaps) == 0 || count.Load() != 0 {
				t.Fatal(result, err, count.Load())
			}
			var valid bool
			if err := s.pool.QueryRow(context.Background(), `SELECT NOT EXISTS(SELECT 1 FROM background_usage WHERE owner_id=$1) AND EXISTS(SELECT 1 FROM background_stage_events WHERE owner_id=$1 AND stage='query_embedding' AND count=1)`, string(scope.OwnerID)).Scan(&valid); err != nil || !valid {
				t.Fatal("fallback was unobserved or charged", valid, err)
			}
			if kind != "unconfigured" {
				row := queryCallRow(t, s, scope)
				if row.Outcome != "failed" || row.Accounting != "not_reserved" || row.Reservation != "" || row.ErrorCode == "" {
					t.Fatal(row)
				}
			}
		})
	}
}

func TestQueryEmbeddingAccountingRestartKeepsOnePaidCallAndDiscardsBody(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_query_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic usage failure'; END $$; CREATE TRIGGER reject_query_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_query_usage()`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
	var persistence *modelcall.PersistenceError
	if !errors.As(err, &persistence) {
		t.Fatal("accounting failure hidden", err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_query_usage ON model_usage`); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	status, err := peer.recoverInteractiveCallsOnce(ctx)
	row := queryCallRow(t, s, scope)
	if err != nil || status.Accounting != 1 || status.PendingAccounting != 0 || row.Accounting != "settled" || row.Receipt.Available || count.Load() != 1 {
		t.Fatal(status, row, err, count.Load())
	}
	var bodies int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatal("restart retained private query text", bodies, err)
	}
}

func TestQueryEmbeddingDroppedReadSessionRecordsInterruptionAndKeepsLateBilling(t *testing.T) {
	s, scope := testStore(t), owner()
	queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	request, release := queryReadRequest(t, s, scope)
	adapter := interactiveCalls{store: s}
	ctx := context.Background()
	id, err := adapter.Prepare(ctx, request, *request.ProviderSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := adapter.Reserve(ctx, request, *request.ReservationEstimate)
	if err != nil {
		t.Fatal(err)
	}
	paid := &modelcall.PaidResult{InvocationID: id, Prompt: request.Prompt, Provider: request.ProviderID, Model: request.ProviderSnapshot.Model, Reservation: reservation.ID, ReservedCost: reservation.Cost}
	if err := adapter.Start(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	// End the actual session as process exit would. No fictitious lease is involved.
	if err := release(); err != nil {
		t.Fatal(err)
	}
	status, err := s.recoverInteractiveCallsOnce(ctx)
	row := queryCallRow(t, s, scope)
	if err != nil || status.Interrupted != 1 || row.Outcome != "unknown" || row.Accounting != "held" || row.Receipt.Billing != nil {
		t.Fatal(status, row, err)
	}
	paid.Output, paid.InputTokens, paid.Cost = `[{"model":"vector:synthetic-vector","values":[1,0]}]`, 7, 0.000014
	if err := adapter.Save(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	if err := s.calls.RecoverAccounting(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	row = queryCallRow(t, s, scope)
	if row.Outcome != "returned" || row.Accounting != "settled" || row.Receipt.Available || !paid.NotApplicable {
		t.Fatal("late response lost cost or restored private data", row, paid)
	}
	var bodies int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatal(bodies, err)
	}
}

func TestQueryEmbeddingMixedForegroundCapacityKeepsAccountingConnection(t *testing.T) {
	s, scope := testStore(t), owner()
	started, finish := make(chan struct{}, 1), make(chan struct{})
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-finish
		queryEmbeddingReply(w)
	})
	parent := admittedInteractiveRequest(t, s, scope, "vector")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Eight existing foreground sessions and their existing shared slots.
	var held []*pgxpool.Conn
	for range cap(s.secretarySlots) - 1 {
		s.secretarySlots <- struct{}{}
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, conn)
	}
	defer func() {
		for _, conn := range held {
			conn.Release()
			<-s.secretarySlots
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "standalone", Mode: memory.Remember})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		close(finish)
		t.Fatal("standalone call could not start with a journal margin")
	}
	// All slots are occupied. An inherited request must not acquire another one.
	nestedDone := make(chan error, 1)
	go func() {
		_, err := s.Recall(context.WithValue(ctx, interactiveExecutionKey{}, parent), scope, memory.RecallRequest{Query: "nested", Mode: memory.Remember})
		nestedDone <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		close(finish)
		t.Fatal("nested query reacquired capacity or blocked journal progress")
	}
	// A waiting standalone query cancels before any provider call.
	waitCtx, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.Recall(waitCtx, scope, memory.RecallRequest{Query: "waiting", Mode: memory.Remember}); !errors.Is(err, context.Canceled) {
		close(finish)
		t.Fatal(err)
	}
	status, err := s.recoverInteractiveCallsOnce(ctx)
	if err != nil || status.Interrupted != 0 || !status.CountsKnown || count.Load() != 2 {
		close(finish)
		t.Fatal("live sessions were interrupted or capacity started extra work", status, err, count.Load())
	}
	close(finish)
	for _, channel := range []chan error{done, nestedDone} {
		select {
		case err := <-channel:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("accounting did not finish with the retained connection")
		}
	}
}

func TestQueryEmbeddingSavedResultRecoveryRetriesStorageWithoutAnotherProvider(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	request, release := queryReadRequest(t, s, scope)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_query_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic result failure'; END $$; CREATE TRIGGER reject_query_result BEFORE UPDATE ON background_model_results FOR EACH ROW EXECUTE FUNCTION reject_query_result()`); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.calls.Call(work, request); done <- err }()
	for {
		pending := false
		s.pendingInteractive.Range(func(_, _ any) bool { pending = true; return false })
		if pending {
			break
		}
		select {
		case <-work.Done():
			t.Fatal("known result was not retained", work.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	var persistence *modelcall.PersistenceError
	if err := <-done; !errors.As(err, &persistence) {
		t.Fatal("result write failure was hidden", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_query_result ON background_model_results`); err != nil {
		t.Fatal(err)
	}
	s.SetModels(nil)
	status, err := s.recoverInteractiveCallsOnce(ctx)
	row := queryCallRow(t, s, scope)
	if err != nil || status.Results != 1 || status.PendingResults != 0 || row.Accounting != "settled" || row.Receipt.Available || count.Load() != 1 {
		t.Fatal(status, row, err, count.Load())
	}
}

func TestQueryEmbeddingCanceledReadRecordsUnknownOutcomeWithoutAnotherCall(t *testing.T) {
	s, scope := testStore(t), owner()
	started := make(chan struct{})
	finish := make(chan struct{})
	count := queryEmbeddingModel(t, s, func(_ http.ResponseWriter, r *http.Request) {
		var input any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-finish:
		}
	})
	t.Cleanup(func() { close(finish) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		row := queryCallRow(t, s, scope)
		if row.Receipt.Billing != nil && row.Accounting == "held" {
			if row.Outcome != "unknown" || row.Receipt.Available || count.Load() != 1 {
				t.Fatal(row, count.Load())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled invocation did not persist its accounting", row)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.secretarySlots) != 0 {
		t.Fatal("canceled read retained a slot")
	}
}

func TestQueryEmbeddingDeputyKeepsSeparateReservationAndActualRunIdentity(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) { secretaryModelReply(w, "Synthetic reply.") })
	parent := leasedDeputyAnswer(t, s, scope)
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	ctx := context.WithValue(context.Background(), interactiveExecutionKey{}, parent)
	if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember}); err != nil {
		t.Fatal(err)
	}
	row := queryCallRow(t, s, scope)
	var valid bool
	if err := s.pool.QueryRow(ctx, `SELECT reserved_cost=0.17 AND EXISTS(SELECT 1 FROM model_usage WHERE owner_id=$1 AND id=$3 AND run_id=$2 AND purpose='query_embedding') FROM agent_runs WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(parent.ExecutionID), string(row.UsageID)).Scan(&valid); err != nil || !valid || row.Manifest.Kind != "deputy" || row.Manifest.BudgetOwner != "" || row.ExecutionID != parent.ExecutionID || count.Load() != 1 {
		t.Fatal("query charged the admitted deputy hold or fabricated a run", row, valid, err, count.Load())
	}
}

func TestQueryEmbeddingRecallChecksAccessAfterProviderReturns(t *testing.T) {
	s, scope := testStore(t), owner()
	ref := mustIngest(t, s, scope, input()).Ref
	scope.IsOwner, scope.PrincipalID = false, "granted-reader"
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)`, string(scope.OwnerID), string(ref.ID), scope.PrincipalID); err != nil {
		t.Fatal(err)
	}
	queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := s.pool.Exec(ctx, `DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2 AND principal_id=$3`, string(scope.OwnerID), string(ref.ID), scope.PrincipalID); err != nil {
			t.Error(err)
		}
		queryEmbeddingReply(w)
	})
	result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember})
	row := queryCallRow(t, s, scope)
	if err != nil || len(result.Memories) != 0 || len(result.Evidence) != 0 || row.Accounting != "settled" || row.Receipt.Available {
		t.Fatal("read returned data after its grant was removed", result, row, err)
	}
}

func TestQueryEmbeddingFinishedBillingAfterReadExitDiscardsOnlyPrivateBody(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	request, release := queryReadRequest(t, s, scope)
	paid, err := s.calls.Call(context.Background(), request)
	if err != nil || paid == nil {
		t.Fatal(paid, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// Simulate a process that returned the vector but stopped before reporting use.
	peer, err := Open(context.Background(), s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	status, err := peer.recoverInteractiveCallsOnce(context.Background())
	row := queryCallRow(t, s, scope)
	if err != nil || status.Accounting != 1 || status.PendingAccounting != 0 || row.Accounting != "settled" || row.Receipt.Available || row.Receipt.Billing == nil || count.Load() != 1 {
		t.Fatal(status, row, err, count.Load())
	}
	var countRows int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM model_usage WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&countRows); err != nil || countRows != 1 {
		t.Fatal("cleanup changed accounting", countRows, err)
	}
}

func TestQueryEmbeddingDeletedSecretaryOriginCannotRestoreCachedQuery(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	parent := admittedInteractiveRequest(t, s, scope, "vector")
	in := input()
	in.Connector, in.ExternalID, in.Text = "desk", string(parent.ExecutionID), "Synthetic secretary query origin."
	ref := mustIngest(t, s, scope, in).Ref
	ctx := context.WithValue(context.Background(), interactiveExecutionKey{}, parent)
	if _, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
	row := queryCallRow(t, s, scope)
	if err != nil || result.Coverage.Complete || row.Receipt.Available || count.Load() != 1 || row.Accounting != "settled" {
		t.Fatal("deleted input was restored or billed again", result, row, err, count.Load())
	}
	var bodies int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatal("deleted query was retained", bodies, err)
	}
}

func TestQueryEmbeddingExpiredParentDiscardsVectorAndPreservesBilling(t *testing.T) {
	for _, kind := range []string{"secretary", "deputy"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) { secretaryModelReply(w, "Synthetic reply.") })
			var parent modelcall.Request
			if kind == "secretary" {
				parent = admittedInteractiveRequest(t, s, scope, "model")
			} else {
				parent = leasedDeputyAnswer(t, s, scope)
			}
			count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
				query := `UPDATE desk_turn_order SET expires_at=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND request_id=$2`
				if kind == "deputy" {
					query = `UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND id=$2`
				}
				if _, err := s.pool.Exec(context.Background(), query, string(scope.OwnerID), string(parent.ExecutionID)); err != nil {
					t.Error(err)
				}
				queryEmbeddingReply(w)
			})
			ctx := context.WithValue(context.Background(), interactiveExecutionKey{}, parent)
			result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "synthetic", Mode: memory.Remember})
			row := queryCallRow(t, s, scope)
			if err != nil || result.Coverage.Complete || row.Accounting != "settled" || row.Receipt.Available || row.Receipt.Billing == nil || row.Receipt.Billing.Cost != 0.000014 || count.Load() != 1 {
				t.Fatal("expired execution restored query data or lost cost", result, row, err, count.Load())
			}
		})
	}
}
