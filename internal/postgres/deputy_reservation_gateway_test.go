package postgres

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func leasedDeputyAnswer(t *testing.T, s *Store, scope memory.Scope) modelcall.Request {
	t.Helper()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious deputy reservation"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Prepare a fictitious draft."})
	run := state.Runs[0]
	token := string(memory.NewID())
	if _, err := s.pool.Exec(context.Background(), `UPDATE agent_runs SET reserved_cost=0.17,status='running',lease_token=$3,lease_until=clock_timestamp()+interval '5 minutes',document=jsonb_set(document,'{cost}','0.17') WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), run.ID, token); err != nil {
		t.Fatal(err)
	}
	return modelcall.Request{
		OwnerID: scope.OwnerID, ExecutionID: memory.ID(run.ID), RootExecutionID: memory.ID(run.ID),
		Function: "deputy", Stage: "answer", ProviderID: "model", Instructions: prompts.Must("deputy"),
		ContextBuilderVersion: "fictitious-deputy-v1", Prompt: asJSON(map[string]string{"rawPrompt": "Fictitious main generation."}),
		Policy: interactiveCallPolicy{Kind: "deputy", Token: token, Origin: run.CreatedAt, AgentID: run.AgentID, ThingID: run.ThingID, BudgetOwner: "deputy_run", Usage: modelUsage{Purpose: "deputy", RunID: run.ID}},
	}
}

func TestDeputyGatewayReusesAdmissionBudgetAndOriginalUsage(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious paid draft.")
	})
	s.models.Config.Providers[0].InputPerMillion = 1
	s.models.Config.Providers[0].OutputPerMillion = 2
	request := leasedDeputyAnswer(t, s, scope)
	ctx := context.Background()
	paid, err := s.calls.Call(ctx, request)
	if err != nil || paid == nil || paid.ReservedCost != 0.17 || paid.Cost <= 0 {
		t.Fatal(paid, err)
	}
	for range 2 {
		duplicate, err := s.calls.Call(ctx, request)
		if err != nil || duplicate.InvocationID != paid.InvocationID || duplicate.Reservation != paid.Reservation {
			t.Fatal("duplicate accounting changed invocation identity", duplicate, err)
		}
	}
	var budget, usage float64
	var reservations, usages int
	var state, usageRun string
	if err := s.pool.QueryRow(ctx, `SELECT r.reserved_cost::double precision,u.cost::double precision,c.accounting_state,u.run_id::text,(SELECT count(*) FROM background_usage WHERE owner_id=$1),(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM agent_runs r JOIN model_calls c ON(c.owner_id,c.execution_id)=(r.owner_id,r.id) JOIN model_usage u ON(u.owner_id,u.id)=(c.owner_id,c.usage_id) WHERE r.owner_id=$1 AND r.id=$2 AND c.reservation_id=c.usage_id`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&budget, &usage, &state, &usageRun, &reservations, &usages); err != nil || budget != usage || state != "settled" || usageRun != string(request.ExecutionID) || reservations != 0 || usages != 1 || calls.Load() != 1 {
		t.Fatal(budget, usage, state, usageRun, reservations, usages, calls.Load(), err)
	}
}

func TestDeputyGatewayRecoversBillingWithoutSettlingReusedRun(t *testing.T) {
	for _, mode := range []string{"deleted", "reused", "accounting_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				secretaryModelReply(w, "Fictitious original response.")
			})
			s.models.Config.Providers[0].InputPerMillion = 1
			request := leasedDeputyAnswer(t, s, scope)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_deputy_billing() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic billing failure'; END $$; CREATE TRIGGER reject_deputy_billing BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_deputy_billing()`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.calls.Call(ctx, request); err == nil {
				t.Fatal("billing failure was hidden")
			}
			if mode == "deleted" {
				if _, err := s.pool.Exec(ctx, `DELETE FROM agent_runs WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
					t.Fatal(err)
				}
			} else if mode == "reused" {
				if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET reserved_cost=0.23,document=jsonb_set(document,'{createdAt}',to_jsonb($3::text)) WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID), time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_deputy_billing ON model_usage`); err != nil {
				t.Fatal(err)
			}
			peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			status, err := peer.recoverInteractiveCallsOnce(ctx)
			if err != nil || status.Accounting != 1 || calls.Load() != 1 {
				t.Fatal(status, err, calls.Load())
			}
			var oldBudget, cost, newBudget float64
			var state string
			if err := s.pool.QueryRow(ctx, `SELECT c.accounting_state,u.cost::double precision,coalesce((SELECT reserved_cost FROM background_usage b WHERE (b.owner_id,b.id)=(c.owner_id,c.reservation_id)),(SELECT reserved_cost FROM agent_runs r WHERE(r.owner_id,r.id)=(c.owner_id,c.execution_id)))::double precision,coalesce((SELECT reserved_cost FROM agent_runs WHERE owner_id=$1 AND id=$2),0)::double precision FROM model_calls c JOIN model_usage u ON(u.owner_id,u.id)=(c.owner_id,c.usage_id) WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&state, &cost, &oldBudget, &newBudget); err != nil || state != "settled" || oldBudget != cost || cost <= 0 || mode == "reused" && newBudget != 0.23 || mode == "deleted" && newBudget != 0 {
				t.Fatal(state, cost, oldBudget, newBudget, err)
			}
		})
	}
}

func TestDeputyInterruptedReservationDistinguishesUnsubmittedAndUnknown(t *testing.T) {
	for _, stage := range []string{"prepared", "started"} {
		t.Run(stage, func(t *testing.T) {
			s, scope := testStore(t), owner()
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { t.Error("recovery submitted another model call") })
			request := leasedDeputyAnswer(t, s, scope)
			ctx := context.Background()
			provider, _ := s.models.Get("model")
			adapter := interactiveCalls{store: s}
			invocation, err := adapter.Prepare(ctx, request, provider)
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := adapter.Reserve(ctx, request, 0.04)
			if err != nil || reservation.Cost != 0.17 {
				t.Fatal(reservation, err)
			}
			if stage == "started" {
				if err := adapter.Start(ctx, request, &modelcall.PaidResult{InvocationID: invocation, Reservation: reservation.ID}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
				t.Fatal(err)
			}
			status, err := s.recoverInteractiveCallsOnce(ctx)
			if err != nil || status.Interrupted != 1 {
				t.Fatal(status, err)
			}
			var outcome, state string
			var budget float64
			if err := s.pool.QueryRow(ctx, `SELECT c.outcome,c.accounting_state,r.reserved_cost::double precision FROM model_calls c JOIN agent_runs r ON(r.owner_id,r.id)=(c.owner_id,c.execution_id) WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(invocation)).Scan(&outcome, &state, &budget); err != nil {
				t.Fatal(err)
			}
			if stage == "prepared" && (outcome != "failed" || state != "settled" || budget != 0) || stage == "started" && (outcome != "unknown" || state != "held" || budget != 0.17) {
				t.Fatal(outcome, state, budget)
			}
			if _, err := s.calls.Call(ctx, request); !errors.Is(err, modelcall.ErrOutcomeUnknown) && stage == "started" {
				t.Fatal("unknown generation was repeated", err)
			}
		})
	}
}

func TestDeputyCancellationRetainsAdmissionHoldWithoutAnotherGeneration(t *testing.T) {
	s, scope := testStore(t), owner()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	s.models.Config.Providers[0].InputPerMillion = 1
	request := leasedDeputyAnswer(t, s, scope)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.calls.Call(ctx, request); done <- err }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("generation was not submitted")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, modelcall.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled generation did not retain its receipt")
	}
	s.SetModels(nil)
	if _, err := s.calls.Call(context.Background(), request); !errors.Is(err, modelcall.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	var budget, held float64
	var state string
	var usages int
	if err := s.pool.QueryRow(context.Background(), `SELECT r.reserved_cost::double precision,(c.result_receipt->'billing'->>'reservedCost')::double precision,c.accounting_state,(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls c JOIN agent_runs r ON(r.owner_id,r.id)=(c.owner_id,c.execution_id) WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&budget, &held, &state, &usages); err != nil || budget != 0.17 || held != 0.17 || state != "held" || usages != 1 || calls.Load() != 1 {
		t.Fatal(budget, held, state, usages, calls.Load(), err)
	}
}
