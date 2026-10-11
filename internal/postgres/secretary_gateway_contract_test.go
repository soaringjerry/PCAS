package postgres

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

// An OpenAI-compatible provider has no output schema and no web search. The
// secretary validates the reply itself, so such a provider must still answer.
func TestSecretaryOnAProviderWithoutSchemaOrSearchAnswersAndRecordsTheFallback(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	ordinarySecretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"reply":"Fictitious reply.","actions":[{"op":"create_task","title":"Fictitious task"}]}`)
	})
	request := turnRequest("Fictitious ordinary-provider check.")
	out, err := s.DeskTurn(WithMemoryTier(context.Background(), "light"), scope, request)
	if err != nil || calls.Load() != 1 || out.Turn.Reply != "Fictitious reply." || len(out.State.Tasks) != 1 {
		t.Fatal("an ordinary provider did not answer and act", out.Turn, calls.Load(), err)
	}
	var outcome, accounting, schema, search, fallback string
	err = s.pool.QueryRow(t.Context(), `SELECT outcome,accounting_state,schema_name,actual_mode->>'search',actual_mode->>'unsupportedFallback'
 FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='answer'`, string(scope.OwnerID), request.RequestID).Scan(&outcome, &accounting, &schema, &search, &fallback)
	if err != nil || outcome != "returned" || accounting != "settled" || schema != "secretary-output" || search != "false" || fallback != `["output_schema", "web_search"]` {
		t.Fatal("the journal did not record the submitted mode", outcome, accounting, schema, search, fallback, err)
	}
}
