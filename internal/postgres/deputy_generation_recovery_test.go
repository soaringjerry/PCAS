package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDeputyGenerationRecoversWritesWithoutRepeatingPaidWork(t *testing.T) {
	for _, mode := range []string{"ordinary", "review", "rejected_review", "selection_write", "selection_restart", "interrupted_review", "revision"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			var calls atomic.Int32
			want := "Fictitious original draft."
			if mode == "review" || mode == "selection_write" {
				want = "Fictitious checked draft."
			}
			if mode == "revision" {
				want = "Fictitious revised document.\nKeep the original paragraph."
			}
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if mode == "revision" {
					secretaryModelReply(w, `{"body":"Fictitious revised document.\nKeep the original paragraph.","complete":true}`)
					return
				}
				if n == 1 {
					if mode != "ordinary" {
						time.Sleep(500 * time.Millisecond)
					}
					secretaryModelReply(w, "Fictitious original draft.")
					return
				}
				if mode == "rejected_review" {
					secretaryModelReply(w, "- [ ] Unexpected extra task.")
					return
				}
				secretaryModelReply(w, "Fictitious checked draft.")
			})
			s.models.Config.Providers[0].InputPerMillion = 1
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious recovery task"})
			command := workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Prepare a fictitious draft.", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
			if mode == "revision" {
				doc := workspace.Doc{ID: string(memory.NewID()), ThingID: command.ThingID, Title: "Fictitious document", Body: "Fictitious original document.\nKeep the original paragraph.", By: "user"}
				state = workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &doc})
				command.Kind, command.DocumentID, command.BaseVersion, command.ExpectedRevision = "revise", doc.ID, 1, state.Revision
			}
			tier := "medium"
			if mode == "ordinary" || mode == "revision" {
				tier = "light"
			}
			response, err := s.Execute(WithMemoryTier(ctx, tier), scope, command)
			if err != nil {
				t.Fatal(err)
			}
			run := response.Runs[0]
			trigger := `CREATE FUNCTION reject_deputy_application() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.document->>'status'='done' THEN RAISE EXCEPTION 'synthetic application failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_deputy_application BEFORE UPDATE ON agent_runs FOR EACH ROW EXECUTE FUNCTION reject_deputy_application()`
			if mode == "selection_write" || mode == "selection_restart" {
				trigger = `CREATE FUNCTION reject_deputy_application() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actual_mode ? 'deputySelection' THEN RAISE EXCEPTION 'synthetic selection failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_deputy_application BEFORE UPDATE ON model_calls FOR EACH ROW EXECUTE FUNCTION reject_deputy_application()`
			}
			if _, err := s.pool.Exec(ctx, trigger); err != nil {
				t.Fatal(err)
			}
			if err := s.RunDeputyOnce(ctx); err == nil {
				t.Fatal("write failure was hidden")
			}
			var first interactiveCallRow
			if err := s.pool.QueryRow(ctx, `SELECT to_jsonb(c) FROM model_calls c WHERE owner_id=$1 AND execution_id=$2 AND stage='answer'`, string(scope.OwnerID), run.ID).Scan(&first); err != nil || first.Outcome != "returned" || first.Accounting != "settled" || first.Receipt.Billing == nil {
				t.Fatal(first, err)
			}
			table := "agent_runs"
			if mode == "selection_write" || mode == "selection_restart" {
				table = "model_calls"
			}
			if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_deputy_application ON "+table); err != nil {
				t.Fatal(err)
			}
			if mode == "interrupted_review" {
				if _, err := s.pool.Exec(ctx, `UPDATE model_calls SET actual_mode=actual_mode-'deputySelection' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(first.ID)); err != nil {
					t.Fatal(err)
				}
			}
			worker := s
			if mode != "selection_write" {
				// A new process has no pending responses, model registry, or reviewer.
				worker = &Store{pool: s.pool}
				worker.SetModels(nil)
			}
			if err := worker.RunDeputyOnce(ctx); err != nil {
				t.Fatal("saved work did not recover", err)
			}
			state, err = s.Snapshot(ctx, scope)
			if err != nil || len(state.Runs) != 1 || state.Runs[0].Status != "done" || state.Runs[0].Output != want {
				t.Fatal(state.Runs, err)
			}
			expected := int32(2)
			if tier == "light" {
				expected = 1
			}
			var usage, invocations int
			if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM model_usage WHERE owner_id=$1),(SELECT count(*) FROM model_calls WHERE owner_id=$1)`, string(scope.OwnerID)).Scan(&usage, &invocations); err != nil || calls.Load() != expected || usage != int(expected) || invocations != int(expected) {
				t.Fatal("paid work repeated or usage lost", calls.Load(), usage, invocations, err)
			}
			var final interactiveCallRow
			if err := s.pool.QueryRow(ctx, `SELECT to_jsonb(c) FROM model_calls c WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(first.ID)).Scan(&final); err != nil || final.ID != first.ID || final.Reservation != first.Reservation {
				t.Fatal(final, err)
			}
			if mode != "revision" {
				selected := final.Mode.DeputySelection
				if selected == nil || selected.Main != first.ID || selected.OutputHash != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
					t.Fatal("result decision was not durable", selected)
				}
				if (mode == "selection_restart" || mode == "interrupted_review") && (selected.Fallback != "selfcheck_interrupted" || selected.Review == "") {
					t.Fatal("unrecorded review decision was promoted", selected)
				}
				if mode == "rejected_review" && selected.Fallback != "selfcheck_output_rejected" {
					t.Fatal(selected)
				}
			} else {
				doc, err := s.pool.Query(ctx, `SELECT document->>'body' FROM work_documents WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), command.DocumentID)
				if err != nil {
					t.Fatal(err)
				}
				defer doc.Close()
				if !doc.Next() {
					t.Fatal("revision missing")
				}
				var body string
				if err := doc.Scan(&body); err != nil || body != want {
					t.Fatal(body, err)
				}
			}
		})
	}
}

// A deputy on a provider without web search still completes its run. The
// journal records that search was not submitted.
func TestDeputyOnAProviderWithoutSearchCompletesAndRecordsTheFallback(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	ordinarySecretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious draft result.")
	})
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious ordinary deputy"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Fictitious draft."})
	if err := s.RunDeputyOnce(WithMemoryTier(context.Background(), "light")); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || state.Runs[0].Status != "done" || !strings.Contains(state.Runs[0].Output, "Fictitious draft result.") || calls.Load() == 0 {
		t.Fatal(state.Runs, calls.Load(), err)
	}
	var search, fallback string
	if err := s.pool.QueryRow(context.Background(), `SELECT actual_mode->>'search',actual_mode->>'unsupportedFallback' FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='answer' AND outcome='returned'`, string(scope.OwnerID), state.Runs[0].ID).Scan(&search, &fallback); err != nil || search != "false" || fallback != `["web_search"]` {
		t.Fatal("the journal did not record the submitted mode", search, fallback, err)
	}
}

func TestDeputyInterruptedSubmissionRetainsHoldAndNeverRetries(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		secretaryModelReply(w, "Interrupted fictitious response.")
	})
	s.models.Config.Providers[0].InputPerMillion = 1
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious interrupted work"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Prepare fictitious work."})
	run := state.Runs[0]
	if err := s.RunDeputyOnce(ctx); err == nil {
		t.Fatal("interruption was hidden")
	}
	if err := s.RunDeputyOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status, outcome, accounting string
	var application, reason string
	var held float64
	if err := s.pool.QueryRow(context.Background(), `SELECT r.status,r.reserved_cost::double precision,c.outcome,c.accounting_state,c.actual_mode->>'applicationOutcome',c.actual_mode->>'applicationReason' FROM agent_runs r JOIN model_calls c ON(c.owner_id,c.execution_id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND c.stage='answer'`, string(scope.OwnerID), run.ID).Scan(&status, &held, &outcome, &accounting, &application, &reason); err != nil || status != "failed" || outcome != "unknown" || accounting != "held" || held < run.Cost || held <= 0 || calls.Load() != 1 || application != "not_applicable" || reason != "provider_outcome_unknown" {
		t.Fatal(status, held, outcome, accounting, application, reason, calls.Load(), err)
	}
}

func TestDeputyAbandonedInputRecordsSkippedApplication(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("abandoned input cannot start generation")
	})
	request := leasedDeputyAnswer(t, s, scope)
	ctx := context.Background()
	adapter := interactiveCalls{store: s}
	provider, _ := s.models.Get(request.ProviderID)
	id, err := adapter.Prepare(ctx, request, provider)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := adapter.Reserve(ctx, request, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	paid := &modelcall.PaidResult{InvocationID: id, Reservation: reservation.ID, ReservedCost: reservation.Cost, Provider: provider.ID, Model: provider.Model, Prompt: request.Prompt}
	if err := adapter.Start(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.recordInterruptedExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.RunDeputyOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var status, application, reason, outcome, accounting string
	var held float64
	if err := s.pool.QueryRow(ctx, `SELECT r.status,r.reserved_cost::double precision,c.outcome,c.accounting_state,c.actual_mode->>'applicationOutcome',c.actual_mode->>'applicationReason' FROM agent_runs r JOIN model_calls c ON(c.owner_id,c.execution_id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(id)).Scan(&status, &held, &outcome, &accounting, &application, &reason); err != nil || status != "failed" || held != 0.17 || outcome != "unknown" || accounting != "held" || application != "not_applicable" || reason != "deputy_execution_interrupted" || calls.Load() != 0 {
		t.Fatal(status, held, outcome, accounting, application, reason, calls.Load(), err)
	}
}
