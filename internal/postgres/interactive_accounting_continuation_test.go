package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestInteractiveWorkerRecoversDeletedInputAccountingAfterRestart(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	request.Refs = []memory.Ref{organizeTestMemory(t, s, scope, "Fictitious input for accounting recovery.")}
	original := request.Policy.(interactiveCallPolicy).Usage
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_continuation_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic usage failure'; END $$; CREATE TRIGGER reject_continuation_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_continuation_usage()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(ctx, request); err == nil {
		t.Fatal("accounting failure was hidden")
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: request.Refs}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_continuation_usage ON model_usage; UPDATE desk_turn_order SET status='canceled'`); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- peer.RunAgents(runCtx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	state := ""
	for state != "settled" {
		if err := s.pool.QueryRow(runCtx, `SELECT accounting_state FROM model_calls WHERE owner_id=$1 AND execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "settled" {
			select {
			case <-runCtx.Done():
				t.Fatal("worker did not finish accounting", state, runCtx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var usageID, turn, tier string
	var bodies, usages int
	var available bool
	if err := s.pool.QueryRow(ctx, `SELECT c.usage_id::text,u.turn_id::text,u.tier,(c.result_receipt->>'availableForApplication')::boolean,(SELECT count(*) FROM background_model_results WHERE owner_id=$1),(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls c JOIN model_usage u ON u.owner_id=c.owner_id AND u.id=c.usage_id WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&usageID, &turn, &tier, &available, &bodies, &usages); err != nil || usageID != string(original.ID) || turn != original.TurnID || tier != original.Tier || available || bodies != 0 || usages != 1 || calls.Load() != 1 {
		t.Fatal(usageID, turn, tier, available, bodies, usages, calls.Load(), err)
	}
	status, err := peer.recoverInteractiveCallsOnce(ctx)
	if err != nil || status.Accounting != 0 || status.Results != 0 || status.Interrupted != 0 || status.PendingAccounting != 0 || !status.CountsKnown {
		t.Fatal(status, err)
	}
}

func TestInteractiveWorkerRejectsResponseFromReplacedDeputyLease(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		t.Error("accounting recovery submitted a provider call")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious deputy task"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "summary", Prompt: "Fictitious deputy request."})
	if len(state.Runs) != 1 {
		t.Fatal(state.Runs)
	}
	run := state.Runs[0]
	ctx := context.Background()
	token := string(memory.NewID())
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET status='running',lease_token=$3,lease_until=clock_timestamp()+interval '5 minutes' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), run.ID, token); err != nil {
		t.Fatal(err)
	}
	policy := request.Policy.(interactiveCallPolicy)
	policy.Kind, policy.Token, policy.Origin = "deputy", token, run.CreatedAt
	policy.RequestHash = nil
	policy.ThingID = run.ThingID
	policy.Usage.RunID = run.ID
	request.Policy = policy
	request.ExecutionID, request.RootExecutionID = memory.ID(run.ID), memory.ID(run.ID)
	adapter := interactiveCalls{store: s}
	provider, _ := s.models.Get("model")
	invocation, err := adapter.Prepare(ctx, request, provider)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := adapter.Reserve(ctx, request, 0.02)
	if err != nil {
		t.Fatal(err)
	}
	paid := &modelcall.PaidResult{InvocationID: invocation, Reservation: reservation, ReservedCost: 0.02, Provider: provider.ID, Model: provider.Model, Prompt: request.Prompt, Cost: 0.001, Output: "Fictitious late deputy response."}
	if err := adapter.Start(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	active, err := s.recoverInteractiveCallsOnce(ctx)
	if err != nil || active.Interrupted != 0 {
		t.Fatal("active deputy was interrupted", active, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET lease_token=$3 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), run.ID, string(memory.NewID())); err != nil {
		t.Fatal(err)
	}
	s.SetModels(nil)
	interrupted, err := s.recoverInteractiveCallsOnce(ctx)
	if err != nil || interrupted.Interrupted != 1 {
		t.Fatal(interrupted, err)
	}
	if err := adapter.Save(ctx, request, paid); err != nil || !paid.NotApplicable {
		t.Fatal("replaced deputy retained application rights", paid.NotApplicable, err)
	}
	for range 2 {
		if _, err := s.recoverInteractiveCallsOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var usageRun, accounting, outcome string
	var count int
	var available bool
	var cost float64
	if err := s.pool.QueryRow(ctx, `SELECT u.run_id::text,c.accounting_state,c.actual_mode->>'interruptedOutcome',(c.result_receipt->>'availableForApplication')::boolean,b.reserved_cost::double precision,(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls c JOIN model_usage u ON u.owner_id=c.owner_id AND u.id=c.usage_id JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(invocation)).Scan(&usageRun, &accounting, &outcome, &available, &cost, &count); err != nil || usageRun != run.ID || accounting != "settled" || outcome != "unknown" || available || cost != paid.Cost || count != 1 {
		t.Fatal(usageRun, accounting, outcome, available, cost, count, err)
	}
}

func TestInteractiveWorkerSavesResponseWithoutRepeatingCanceledRequest(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious returned response.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_continuation_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic result failure'; END $$; CREATE TRIGGER reject_continuation_result BEFORE INSERT ON background_model_results FOR EACH ROW EXECUTE FUNCTION reject_continuation_result()`); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.calls.Call(work, request); done <- err }()
	pending := false
	for !pending {
		s.pendingInteractive.Range(func(_, _ any) bool { pending = true; return false })
		if !pending {
			select {
			case <-work.Done():
				t.Fatal(work.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("persistence failure was hidden")
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_continuation_result ON background_model_results; UPDATE desk_turn_order SET status='canceled'`); err != nil {
		t.Fatal(err)
	}
	s.SetModels(nil)
	status, err := s.recoverInteractiveCallsOnce(ctx)
	if err != nil || status.Results != 1 || status.PendingResults != 0 || status.PendingAccounting != 0 || !status.CountsKnown || calls.Load() != 1 {
		t.Fatal(status, err, calls.Load())
	}
	if _, err := s.calls.Call(ctx, request); !errors.Is(err, modelcall.ErrNotApplicable) {
		t.Fatal("canceled result became applicable", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_usage WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestInteractiveWorkerClassifiesInterruptedCallsWithoutResubmission(t *testing.T) {
	for _, stage := range []string{"prepared", "started"} {
		t.Run(stage, func(t *testing.T) {
			s, scope := testStore(t), owner()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
			request := admittedInteractiveRequest(t, s, scope, "model")
			ctx := context.Background()
			adapter := interactiveCalls{store: s}
			provider, _ := s.models.Get("model")
			invocation, err := adapter.Prepare(ctx, request, provider)
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := adapter.Reserve(ctx, request, 0.02)
			if err != nil {
				t.Fatal(err)
			}
			paid := &modelcall.PaidResult{InvocationID: invocation, Reservation: reservation, ReservedCost: 0.02, Provider: provider.ID, Model: provider.Model, Prompt: request.Prompt, Cost: 0.001}
			if stage == "started" {
				if err := adapter.Start(ctx, request, paid); err != nil {
					t.Fatal(err)
				}
			}
			// A distant business clock cannot expire a real operational admission.
			s.SetBusinessClock(func() time.Time { return time.Now().Add(365 * 24 * time.Hour) })
			active, err := s.recoverInteractiveCallsOnce(ctx)
			if err != nil || active.Interrupted != 0 {
				t.Fatal("active execution was declared interrupted", active, err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE desk_turn_order SET status='canceled' WHERE owner_id=$1 AND request_id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
				t.Fatal(err)
			}
			s.SetModels(nil)
			status, err := s.recoverInteractiveCallsOnce(ctx)
			if err != nil || status.Interrupted != 1 || status.PendingInterrupted != 0 || !status.CountsKnown {
				t.Fatal(status, err)
			}
			var outcome, code, accounting string
			var budget float64
			var usages, attempt int
			if err := s.pool.QueryRow(ctx, `SELECT c.outcome,c.error_code,c.accounting_state,b.reserved_cost::double precision,c.attempt_number,(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls c JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(invocation)).Scan(&outcome, &code, &accounting, &budget, &attempt, &usages); err != nil || calls.Load() != 0 || attempt != 1 || usages != 0 {
				t.Fatal(outcome, code, accounting, budget, attempt, usages, calls.Load(), err)
			}
			if stage == "prepared" {
				if outcome != "failed" || code != "provider_not_started" || accounting != "settled" || budget != 0 {
					t.Fatal(outcome, code, accounting, budget)
				}
				_, err = s.calls.Call(ctx, request)
				var failure *modelcall.Failure
				if !errors.As(err, &failure) || failure.Code != "provider_not_started" {
					t.Fatal("never-submitted call became unknown", err)
				}
			} else {
				if outcome != "unknown" || code != modelcall.ErrOutcomeUnknown.Error() || accounting != "held" || budget != 0.02 {
					t.Fatal(outcome, code, accounting, budget)
				}
				// An actual late response can finish its own accounting, but cannot act.
				paid.Output = "Fictitious late response."
				if err := adapter.Save(ctx, request, paid); err != nil || !paid.NotApplicable {
					t.Fatal(paid.NotApplicable, err)
				}
				recovered, err := s.recoverInteractiveCallsOnce(ctx)
				if err != nil || recovered.Accounting != 1 {
					t.Fatal(recovered, err)
				}
				var available bool
				var interrupted string
				if err := s.pool.QueryRow(ctx, `SELECT c.accounting_state,c.actual_mode->>'interruptedOutcome',(c.result_receipt->>'availableForApplication')::boolean,b.reserved_cost::double precision FROM model_calls c JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(invocation)).Scan(&accounting, &interrupted, &available, &budget); err != nil || accounting != "settled" || interrupted != "unknown" || available || budget != paid.Cost {
					t.Fatal(accounting, interrupted, available, budget, err)
				}
			}
		})
	}
}
