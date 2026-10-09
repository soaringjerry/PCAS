package postgres

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSecretaryRequiredModesFailBeforeProviderCall(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	ordinarySecretaryModel(t, s, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	request := turnRequest("Fictitious required-mode check.")
	out, err := s.DeskTurn(WithMemoryTier(context.Background(), "light"), scope, request)
	if err != nil || calls.Load() != 0 || len(out.Turn.Receipts) != 1 || !strings.Contains(out.Turn.Receipts[0].Text, "不支持") {
		t.Fatal("unsupported mode was invoked or hidden", out.Turn, calls.Load(), err)
	}
	var outcome, accounting, application, schema string
	var reservations, usages int
	err = s.pool.QueryRow(t.Context(), `SELECT outcome,accounting_state,actual_mode->>'applicationOutcome',schema_name,
 (SELECT count(*) FROM background_usage WHERE owner_id=$1),
 (SELECT count(*) FROM model_usage WHERE owner_id=$1)
 FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='answer'`, string(scope.OwnerID), request.RequestID).Scan(&outcome, &accounting, &application, &schema, &reservations, &usages)
	if err != nil || outcome != "failed" || accounting != "not_reserved" || application != "not_applicable" || schema != "secretary-output" || reservations != 0 || usages != 0 {
		t.Fatal("unsupported invocation had no exact failure receipt", outcome, accounting, application, schema, reservations, usages, err)
	}
	if repeated := mustTurn(t, s, scope, request); calls.Load() != 0 || len(repeated.Turn.Receipts) != 1 || repeated.Turn.Receipts[0].Text != out.Turn.Receipts[0].Text {
		t.Fatal("request replay replaced an unsupported invocation", repeated.Turn, calls.Load())
	}
	if len(out.State.Tasks) != 0 || len(out.State.Projects) != 0 {
		t.Fatal("unsupported mode applied business actions", out.State.Tasks, out.State.Projects)
	}
}
