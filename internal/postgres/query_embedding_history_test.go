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

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestQueryEmbeddingDeletingHistorySourceRemovesDependentPrivateInput(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Synthetic informational answer.","actions":[],"remember":false}`)
	})
	vector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) }))
	t.Cleanup(vector.Close)
	s.models.Config.Embedding = "vector"
	s.models.Config.Providers = append(s.models.Config.Providers, ai.Provider{ID: "vector", Model: "synthetic-vector", Protocol: "openai", BaseURL: vector.URL, Embedding: true, InputPerMillion: 2})
	firstRequest := turnRequest("Fictitious private history: violet valley marker.")
	first := mustTurn(t, s, scope, firstRequest)
	secondRequest := turnRequest("Please answer an unrelated informational question.")
	secondRequest.ConversationID = &first.ConversationID
	mustTurn(t, s, scope, secondRequest)
	ctx := context.Background()
	var input string
	if err := s.pool.QueryRow(ctx, `SELECT b.prompt::text FROM background_model_results b JOIN model_calls c ON(c.owner_id,c.id)=(b.owner_id,b.job_id) WHERE c.owner_id=$1 AND c.execution_id=$2 AND c.function_name='query_embedding'`, string(scope.OwnerID), secondRequest.RequestID).Scan(&input); err != nil || !strings.Contains(input, firstRequest.Text) {
		t.Fatal("fixture did not retain the original history in query input", err)
	}
	var ref memory.Ref
	ref.Kind = memory.SourceKind
	if err := s.pool.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON(r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk' AND s.external_id=$2`, string(scope.OwnerID), firstRequest.RequestID).Scan(&ref.ID, &ref.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	var bodies int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results b JOIN model_calls c ON(c.owner_id,c.id)=(b.owner_id,b.job_id) WHERE c.owner_id=$1 AND c.execution_id=$2 AND c.function_name='query_embedding'`, string(scope.OwnerID), secondRequest.RequestID).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatal("deleting history left private query input in another execution", bodies, err)
	}
}

func TestQueryEmbeddingDeletedInputBeforeSubmissionStartsNoProvider(t *testing.T) {
	s, scope := testStore(t), owner()
	count := queryEmbeddingModel(t, s, func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) })
	ref := mustIngest(t, s, scope, input()).Ref
	request, release := queryReadRequest(t, s, scope)
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	request.Refs = []memory.Ref{ref}
	if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(context.Background(), request); !errors.Is(err, modelcall.ErrNotApplicable) || count.Load() != 0 {
		t.Fatal("deleted lookup input was submitted", err, count.Load())
	}
	row := queryCallRow(t, s, scope)
	var valid bool
	if err := s.pool.QueryRow(context.Background(), `SELECT accounting_state='settled' AND NOT EXISTS(SELECT 1 FROM background_model_results b WHERE(b.owner_id,b.job_id)=(c.owner_id,c.id)) AND NOT EXISTS(SELECT 1 FROM model_usage u WHERE(u.owner_id,u.id)=(c.owner_id,c.usage_id)) AND EXISTS(SELECT 1 FROM background_usage b WHERE(b.owner_id,b.id)=(c.owner_id,c.reservation_id) AND b.reserved_cost=0) FROM model_calls c WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(row.ID)).Scan(&valid); err != nil || !valid {
		t.Fatal("unsubmitted query retained input, spend, or a reservation", valid, err)
	}
}

func TestQueryEmbeddingDeletedHistoryDuringProviderReturnKeepsOnlyBilling(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Synthetic informational answer.","actions":[],"remember":false}`)
	})
	firstRequest := turnRequest("Fictitious private history: copper lake marker.")
	first := mustTurn(t, s, scope, firstRequest)
	ctx := context.Background()
	var ref memory.Ref
	ref.Kind = memory.SourceKind
	if err := s.pool.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON(r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk' AND s.external_id=$2`, string(scope.OwnerID), firstRequest.RequestID).Scan(&ref.ID, &ref.Version); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	vector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
			t.Error(err)
		}
		queryEmbeddingReply(w)
	}))
	t.Cleanup(vector.Close)
	s.models.Config.Embedding = "vector"
	s.models.Config.Providers = append(s.models.Config.Providers, ai.Provider{ID: "vector", Model: "synthetic-vector", Protocol: "openai", BaseURL: vector.URL, Embedding: true, InputPerMillion: 2})
	secondRequest := turnRequest("Please answer an unrelated informational question.")
	secondRequest.ConversationID = &first.ConversationID
	mustTurn(t, s, scope, secondRequest)
	row := queryCallRow(t, s, scope)
	if row.ExecutionID != memory.ID(secondRequest.RequestID) || row.Accounting != "settled" || row.Receipt.Available || row.Receipt.Billing == nil || calls.Load() != 1 {
		t.Fatal("late history result restored private data or lost billing", row, calls.Load())
	}
	var valid bool
	if err := s.pool.QueryRow(ctx, `SELECT coalesce((actual_mode->>'inputDeleted')::boolean,false) AND NOT EXISTS(SELECT 1 FROM background_model_results b WHERE(b.owner_id,b.job_id)=(c.owner_id,c.id)) AND EXISTS(SELECT 1 FROM model_usage u WHERE(u.owner_id,u.id)=(c.owner_id,c.usage_id)) FROM model_calls c WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(row.ID)).Scan(&valid); err != nil || !valid {
		t.Fatal("late query body was retained or accounting was erased", valid, err)
	}
}

func TestQueryEmbeddingHistoryReferencesKeepOriginalVersionAndOwnerBoundary(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Synthetic answer.","actions":[],"remember":false}`)
	})
	req := turnRequest("Fictitious original question.")
	turn := mustTurn(t, s, scope, req)
	in := memory.IngestRequest{Connector: "desk", ExternalID: req.RequestID, ExternalVersion: "edited", Text: "Fictitious later edit.", MediaType: "text/plain"}
	edited := mustIngest(t, s, scope, in).Ref
	if edited.Version <= 1 {
		t.Fatal("fixture did not create a later source version", edited)
	}
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, selector := range []struct{ turns, requests []string }{{[]string{turn.Turn.ID}, nil}, {nil, []string{req.RequestID}}} {
			refs, err := queryHistorySourceRefsTx(ctx, tx, scope.OwnerID, selector.turns, selector.requests)
			if err != nil || len(refs) != 1 || refs[0].ID != edited.ID || refs[0].Version != 1 {
				t.Fatal("history claimed a later edit supplied the original question", refs, err)
			}
			other, err := queryHistorySourceRefsTx(ctx, tx, memory.NewID(), selector.turns, selector.requests)
			if err != nil || len(other) != 0 {
				t.Fatal("history reference crossed the owner boundary", other, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	vector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { queryEmbeddingReply(w) }))
	t.Cleanup(vector.Close)
	s.models.Config.Embedding = "vector"
	s.models.Config.Providers = append(s.models.Config.Providers, ai.Provider{ID: "vector", Model: "synthetic-vector", Protocol: "openai", BaseURL: vector.URL, Embedding: true, InputPerMillion: 2})
	next := turnRequest("Continue the original discussion.")
	next.ConversationID = &turn.ConversationID
	mustTurn(t, s, scope, next)
	var used bool
	if err := s.pool.QueryRow(ctx, `SELECT actual_mode->>'applicationOutcome'='used' FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND function_name='query_embedding'`, string(scope.OwnerID), next.RequestID).Scan(&used); err != nil || !used {
		t.Fatal("an active edited source forced a historical query into fallback", used, err)
	}
}

func TestQueryEmbeddingDeputyHandoffRecordsOriginalQuestionSource(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Synthetic handoff answer.","actions":[],"remember":false}`)
	})
	req := turnRequest("Fictitious discussion to pass to the deputy.")
	turn := mustTurn(t, s, scope, req)
	var query string
	vector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Input []string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Input) != 1 {
			t.Error("invalid query input", body, err)
		} else {
			query = body.Input[0]
		}
		queryEmbeddingReply(w)
	}))
	t.Cleanup(vector.Close)
	s.models.Config.Embedding = "vector"
	s.models.Config.Providers = append(s.models.Config.Providers, ai.Provider{ID: "vector", Model: "synthetic-vector", Protocol: "openai", BaseURL: vector.URL, Embedding: true, InputPerMillion: 2})
	command := workspace.Command{RequestID: string(memory.NewID()), Type: "delegateTask", ID: string(memory.NewID()), Title: "Synthetic handoff", AgentID: "model", Kind: "plan", Prompt: "Continue the discussion.", DeskTurnIDs: []string{turn.Turn.ID}}
	// Prepare uses the actual selected handoff history before a deputy run exists.
	if _, err := s.prepareRunContext(context.Background(), scope, command); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, req.Text) || !strings.Contains(query, command.Prompt) {
		t.Fatal("handoff query omitted the selected discussion or current command", query)
	}
	row := queryCallRow(t, s, scope)
	var valid bool
	if err := s.pool.QueryRow(context.Background(), `SELECT input_manifest->>'commandOrigin'=$3 AND input_manifest->>'sourceCoverage'='prepared_query_with_caller_refs' AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.input_manifest->'memoryRefs') ref JOIN sources s ON s.owner_id=c.owner_id AND s.id::text=ref->>'id' WHERE s.connector='desk' AND s.external_id=$4 AND ref->>'version'='1') FROM model_calls c WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(row.ID), command.RequestID, req.RequestID).Scan(&valid); err != nil || !valid {
		t.Fatal("handoff query lost its source or actual command identity", valid, err)
	}
}
