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
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDeputyKnownAnswerSurvivesLeaseChangeButNotExecutionReplacement(t *testing.T) {
	for _, mode := range []string{"expired", "new_lease", "discarded", "deleted", "reused"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			var calls atomic.Int32
			var request modelcall.Request
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var err error
				switch mode {
				case "expired":
					_, err = s.pool.Exec(ctx, `UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID))
				case "new_lease":
					_, err = s.pool.Exec(ctx, `UPDATE agent_runs SET lease_token=$3 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID), string(memory.NewID()))
				case "discarded":
					_, err = s.pool.Exec(ctx, `UPDATE agent_runs SET status='failed' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID))
				case "deleted":
					_, err = s.pool.Exec(ctx, `DELETE FROM agent_runs WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID))
				case "reused":
					_, err = s.pool.Exec(ctx, `UPDATE agent_runs SET document=jsonb_set(document,'{createdAt}',to_jsonb($3::text)),reserved_cost=0.23 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(request.ExecutionID), time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano))
				}
				if err != nil {
					t.Error(err)
				}
				secretaryModelReply(w, "Fictitious known answer.")
			})
			s.models.Config.Providers[0].InputPerMillion = 1
			request = leasedDeputyAnswer(t, s, scope)
			paid, err := s.calls.Call(ctx, request)
			retained := mode == "expired" || mode == "new_lease"
			if retained && (err != nil || paid == nil || paid.Output != "Fictitious known answer.") || !retained && !errors.Is(err, modelcall.ErrNotApplicable) {
				t.Fatal("result retention ignored execution identity", mode, paid, err)
			}
			peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			if _, err := peer.recoverInteractiveCallsOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if retained {
				// Recovery reads the paid result without a configured provider.
				recovered, err := peer.calls.Call(ctx, request)
				if err != nil || recovered.InvocationID != paid.InvocationID || recovered.Output != paid.Output {
					t.Fatal("restart lost a known result", recovered, err)
				}
			}
			var available bool
			var usageCount, bodies int
			if err := s.pool.QueryRow(ctx, `SELECT (result_receipt->>'availableForApplication')::boolean,(SELECT count(*) FROM model_usage WHERE owner_id=$1),(SELECT count(*) FROM background_model_results WHERE owner_id=$1) FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='answer'`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&available, &usageCount, &bodies); err != nil || available != retained || usageCount != 1 || bodies != map[bool]int{true: 1, false: 0}[retained] || calls.Load() != 1 {
				t.Fatal(available, retained, usageCount, bodies, calls.Load(), err)
			}
		})
	}
}

func TestDeputyReturnedAnswerDistinguishesCorrectionFromLostInputAccess(t *testing.T) {
	for _, mode := range []string{"corrected", "revoked", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			var calls atomic.Int32
			var ref memory.Ref
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				state, err := s.Snapshot(ctx, scope)
				if err == nil {
					command := workspace.Command{ID: string(ref.ID), RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
					switch mode {
					case "corrected":
						command.Type, command.Text = "editMemory", "Fictitious corrected input."
					case "revoked":
						command.Type, command.AgentIDs = "setMemoryVisibility", []string{}
					case "deleted":
						command.Type = "deleteMemory"
					}
					_, err = s.Execute(ctx, scope, command)
				}
				if err != nil {
					t.Error("input owner operation failed", err)
					http.Error(w, "synthetic owner operation failure", http.StatusInternalServerError)
					return
				}
				secretaryModelReply(w, "Fictitious result based on original input.")
			})
			ref = organizeTestMemory(t, s, scope, "Fictitious original input.")
			request := leasedDeputyAnswer(t, s, scope)
			request.Refs = []memory.Ref{ref}
			paid, err := s.calls.Call(ctx, request)
			if mode == "corrected" {
				if err != nil || paid == nil || len(paid.Refs) != 1 || paid.Refs[0] != ref {
					t.Fatal("correction erased original output or changed its input identity", paid, err)
				}
			} else if !errors.Is(err, modelcall.ErrNotApplicable) {
				t.Fatal("inaccessible result retained application eligibility", mode, err)
			}
			var available bool
			var bodies, usageCount int
			if err := s.pool.QueryRow(ctx, `SELECT (result_receipt->>'availableForApplication')::boolean,(SELECT count(*) FROM background_model_results WHERE owner_id=$1),(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='answer'`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&available, &bodies, &usageCount); err != nil || available != (mode == "corrected") || bodies != map[bool]int{true: 1, false: 0}[mode == "corrected"] || usageCount != 1 || calls.Load() != 1 {
				t.Fatal(mode, available, bodies, usageCount, calls.Load(), err)
			}
		})
	}
}
