package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestDeputyInputIsDurableBeforeProviderSubmission(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	var calls atomic.Int32
	var request modelcall.Request
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var prompt json.RawMessage
		var purpose, output, outcome string
		var billingAbsent bool
		err := s.pool.QueryRow(ctx, `SELECT b.prompt,b.purpose,b.output,c.outcome,c.result_receipt IS NULL
 FROM model_calls c JOIN background_model_results b ON(b.owner_id,b.job_id)=(c.owner_id,c.id)
 WHERE c.owner_id=$1 AND c.execution_id=$2 AND b.reservation_id=c.reservation_id`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&prompt, &purpose, &output, &outcome, &billingAbsent)
		var prepared, original map[string]string
		if err != nil || json.Unmarshal(prompt, &prepared) != nil || json.Unmarshal(request.Prompt, &original) != nil || prepared["rawPrompt"] != original["rawPrompt"] || purpose != "deputy_input" || output != "" || outcome != "started" || !billingAbsent {
			t.Error("provider submission lacked its original private input", err, purpose, output, outcome, billingAbsent)
			http.Error(w, "synthetic input receipt failure", http.StatusInternalServerError)
			return
		}
		secretaryModelReply(w, "Fictitious returned deputy result.")
	})
	request = leasedDeputyAnswer(t, s, scope)
	paid, err := s.calls.Call(ctx, request)
	if err != nil || paid == nil || calls.Load() != 1 {
		t.Fatal(paid, err, calls.Load())
	}
	var output, purpose string
	if err := s.pool.QueryRow(ctx, `SELECT purpose,output FROM background_model_results WHERE owner_id=$1 AND job_id=$2`, string(scope.OwnerID), string(paid.InvocationID)).Scan(&purpose, &output); err != nil || purpose != "deputy" || output != paid.Output {
		t.Fatal("returned result did not complete its input receipt", purpose, output, err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	recovered, err := peer.calls.Call(ctx, request)
	if err != nil || recovered.Output != paid.Output || recovered.InvocationID != paid.InvocationID {
		t.Fatal("restart did not recover the completed result", recovered, err)
	}
}

func TestDeputyInputWriteFailurePreventsSubmissionAndReleasesAdmission(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "This call must not be submitted.")
	})
	request := leasedDeputyAnswer(t, s, scope)
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_deputy_input() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic private input write failure'; END $$;
 CREATE TRIGGER reject_deputy_input BEFORE INSERT ON background_model_results FOR EACH ROW EXECUTE FUNCTION reject_deputy_input()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(ctx, request); err == nil {
		t.Fatal("input write failure was hidden")
	}
	var outcome, state string
	var budget float64
	var started bool
	var usages, payloads int
	err := s.pool.QueryRow(ctx, `SELECT c.outcome,c.accounting_state,c.started_at IS NOT NULL,r.reserved_cost::double precision,
 (SELECT count(*) FROM model_usage WHERE owner_id=$1),(SELECT count(*) FROM background_model_results WHERE owner_id=$1)
 FROM model_calls c JOIN agent_runs r ON(r.owner_id,r.id)=(c.owner_id,c.execution_id) WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&outcome, &state, &started, &budget, &usages, &payloads)
	if err != nil || outcome != "failed" || state != "settled" || started || budget != 0 || usages != 0 || payloads != 0 || calls.Load() != 0 {
		t.Fatal("failed input persistence left a call or an admission hold", outcome, state, started, budget, usages, payloads, calls.Load(), err)
	}
}

func TestDeputyInterruptedInputCannotBecomeARecoveredResult(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		t.Error("interrupted input recovery submitted another model call")
	})
	request := leasedDeputyAnswer(t, s, scope)
	provider, _ := s.models.Get("model")
	adapter := interactiveCalls{store: s}
	invocation, err := adapter.Prepare(ctx, request, provider)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := adapter.Reserve(ctx, request, 0.04)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx, request, &modelcall.PaidResult{InvocationID: invocation, Reservation: reservation.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	status, err := peer.recoverInteractiveCallsOnce(ctx)
	if err != nil || status.Interrupted != 1 {
		t.Fatal(status, err)
	}
	if _, err := peer.calls.Call(ctx, request); !errors.Is(err, modelcall.ErrOutcomeUnknown) {
		t.Fatal("an input-only receipt became a usable result", err)
	}
	if paid, err := peer.paidModelResult(ctx, worker.Job{OwnerID: scope.OwnerID, ID: invocation}); err != nil || paid != nil {
		t.Fatal("the shared result reader treated input as a paid result", paid, err)
	}
	var outcome, state, purpose, output string
	var budget float64
	var usages int
	err = s.pool.QueryRow(ctx, `SELECT c.outcome,c.accounting_state,b.purpose,b.output,r.reserved_cost::double precision,
 (SELECT count(*) FROM model_usage WHERE owner_id=$1)
 FROM model_calls c JOIN background_model_results b ON(b.owner_id,b.job_id)=(c.owner_id,c.id)
 JOIN agent_runs r ON(r.owner_id,r.id)=(c.owner_id,c.execution_id) WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(invocation)).Scan(&outcome, &state, &purpose, &output, &budget, &usages)
	if err != nil || outcome != "unknown" || state != "held" || purpose != "deputy_input" || output != "" || budget != reservation.Cost || usages != 0 {
		t.Fatal("interrupted input lost its evidence or asserted complete usage", outcome, state, purpose, output, budget, usages, err)
	}
}
