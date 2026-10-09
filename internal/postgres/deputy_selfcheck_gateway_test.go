package postgres

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDeputySelfcheckUsesOriginalRunAndReportsFallback(t *testing.T) {
	for _, mode := range []string{"accepted", "provider_failure", "empty_output", "accounting_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					// Preserve a useful self-check budget under concurrent CI load.
					time.Sleep(500 * time.Millisecond)
					secretaryModelReply(w, "Fictitious original draft.")
					return
				}
				switch mode {
				case "provider_failure":
					http.Error(w, "Fictitious unavailable provider.", http.StatusServiceUnavailable)
				case "empty_output":
					secretaryModelReply(w, "")
				default:
					secretaryModelReply(w, "Fictitious checked draft.")
				}
			})
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"followUps": true})})
			state = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious self-check task"})
			state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "summary", Prompt: "Prepare a fictitious draft."})
			if len(state.Runs) != 1 {
				t.Fatal(state.Runs)
			}
			run := state.Runs[0]
			if mode == "accounting_failure" {
				if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_selfcheck_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.purpose='selfcheck' THEN RAISE EXCEPTION 'synthetic selfcheck accounting failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_selfcheck_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_selfcheck_usage()`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.RunDeputyOnce(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := s.Snapshot(ctx, scope)
			if err != nil || calls.Load() != 2 || len(state.Runs) != 1 || state.Runs[0].Status != "done" || len(state.Notices) != 1 {
				t.Fatal(state.Runs, state.Notices, calls.Load(), err)
			}
			if mode == "accepted" {
				if state.Runs[0].Output != "Fictitious checked draft." || strings.Contains(state.Notices[0].Reason, "原稿") {
					t.Fatal(state.Runs[0], state.Notices[0])
				}
			} else if state.Runs[0].Output != "Fictitious original draft." || !strings.Contains(state.Notices[0].Reason, "保留了原稿") {
				t.Fatal("fallback was hidden or replaced the draft", state.Runs[0], state.Notices[0])
			}
			var count int
			var application string
			if err := s.pool.QueryRow(ctx, `SELECT actual_mode->>'applicationOutcome' FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='selfcheck'`, string(scope.OwnerID), run.ID).Scan(&application); err != nil || (mode == "accepted" && application != "applied") || (mode != "accepted" && application != "skipped") {
				t.Fatal("application outcome was confused with provider success", application, err)
			}
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_stage_events WHERE owner_id=$1 AND stage='selfcheck' AND outcome='failure'`, string(scope.OwnerID)).Scan(&count); err != nil || (mode == "accepted" && count != 0) || (mode != "accepted" && count != 1) {
				t.Fatal("selfcheck fallback did not retain its stage receipt", count, err)
			}
			if mode == "accounting_failure" {
				if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_selfcheck_usage ON model_usage`); err != nil {
					t.Fatal(err)
				}
			}
			s.SetModels(nil)
			recovered, err := s.recoverInteractiveCallsOnce(ctx)
			if err != nil || calls.Load() != 2 || (mode == "accounting_failure" && recovered.Accounting != 1) {
				t.Fatal("selfcheck accounting resubmitted a model", recovered, calls.Load(), err)
			}
			var execution, root, function, stage, prompt, usageRun, accounting string
			var budget, usageCost float64
			if err := s.pool.QueryRow(ctx, `SELECT c.execution_id::text,c.root_execution_id::text,c.function_name,c.stage,c.prompt_name,u.run_id::text,c.accounting_state,b.reserved_cost::double precision,u.cost::double precision FROM model_calls c JOIN model_usage u ON u.owner_id=c.owner_id AND u.id=c.usage_id JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), run.ID).Scan(&execution, &root, &function, &stage, &prompt, &usageRun, &accounting, &budget, &usageCost); err != nil || execution != run.ID || root != run.ID || function != "deputy" || stage != "selfcheck" || prompt != "deputy-selfcheck" || usageRun != run.ID || accounting != "settled" || budget != usageCost {
				t.Fatal(execution, root, function, stage, prompt, usageRun, accounting, budget, usageCost, err)
			}
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_usage WHERE owner_id=$1 AND run_id=$2 AND purpose='selfcheck'`, string(scope.OwnerID), run.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("selfcheck usage was duplicated", count, err)
			}
		})
	}
}
