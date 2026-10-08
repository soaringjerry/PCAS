package postgres

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestModelCallSurvivesApplicationAndKeepsUsageLink(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { organizeTestReply(t, w, r, "event") })
	ref := organizeTestMemory(t, s, scope, "Fictitious plan for a garden report.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	var calls, usages, receipts int
	var outcome, accounting, prompt, hash string
	var manifest map[string]any
	if err := s.pool.QueryRow(ctx, `SELECT c.outcome,c.accounting_state,c.prompt_name,c.instruction_hash,c.input_manifest,
 (SELECT count(*) FROM model_calls WHERE owner_id=$1),
 (SELECT count(*) FROM model_usage u WHERE u.owner_id=c.owner_id AND u.id=c.usage_id),
 (SELECT count(*) FROM background_model_results WHERE owner_id=$1)
 FROM model_calls c WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(j.ID)).Scan(&outcome, &accounting, &prompt, &hash, &manifest, &calls, &usages, &receipts); err != nil {
		t.Fatal(err)
	}
	if outcome != "returned" || accounting != "settled" || prompt != "organize" || len(hash) != 64 || calls != 1 || usages != 1 || receipts != 0 {
		t.Fatal(outcome, accounting, prompt, calls, usages, receipts)
	}
	m, err := s.GetMemory(ctx, scope, string(ref.ID))
	if err != nil || m.Category != "event" {
		t.Fatal(m, err)
	}
	if manifest["coverage"] != "legacy_context_builder" {
		t.Fatal("missing explicit coverage gap")
	}
}

func TestAccountingRecoveryReusesOriginalPaidResultAfterRestart(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); organizeTestReply(t, w, r, "event") })
	ref := organizeTestMemory(t, s, scope, "Fictitious plan for a garden report.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_test_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic accounting failure'; END $$;
 CREATE TRIGGER reject_test_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_test_usage()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err == nil {
		t.Fatal("expected accounting failure")
	}
	var invocation, reservation string
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT id::text,reservation_id::text,accounting_state FROM model_calls WHERE owner_id=$1 AND execution_id=$2`, string(scope.OwnerID), string(j.ID)).Scan(&invocation, &reservation, &state); err != nil || state != "failed" {
		t.Fatal(state, err)
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_test_usage ON model_usage"); err != nil {
		t.Fatal(err)
	}
	// A new Store has no pending cache or configured provider. Recovery must
	// read the durable original result rather than regenerate changed input.
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(nil)
	if err := peer.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	var callCount, usageCount int
	if err := s.pool.QueryRow(ctx, `SELECT accounting_state,
 (SELECT count(*) FROM model_calls WHERE owner_id=$1),
 (SELECT count(*) FROM model_usage WHERE owner_id=$1 AND id=$3)
 FROM model_calls WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), invocation, reservation).Scan(&state, &callCount, &usageCount); err != nil {
		t.Fatal(err)
	}
	if state != "settled" || calls.Load() != 1 || callCount != 1 || usageCount != 1 {
		t.Fatal(state, calls.Load(), callCount, usageCount)
	}
	m, err := s.GetMemory(ctx, scope, string(ref.ID))
	if err != nil || m.Category != "event" {
		t.Fatal(m, err)
	}
}

func TestInterruptedInvocationDoesNotStartAnotherPaidCall(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); secretaryModelReply(w, "unused") })
	organizeTestMemory(t, s, scope, "Fictitious unfinished report.")
	j := organizeTestJob(t, s, scope)
	request := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: j.ID, RootExecutionID: j.ID, Function: "organize", Stage: j.Stage, Instructions: prompts.Must("organize"), Prompt: asJSON(map[string]string{"test": "fictitious"}), Policy: j}
	provider, _ := s.models.Get(s.models.ExtractionID())
	adapter := backgroundCalls{store: s}
	ctx := context.Background()
	invocation, err := adapter.Prepare(ctx, request, provider)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := adapter.Reserve(ctx, request, provider.Reserve(request.Instructions.Text()+string(request.Prompt)))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx, request, &modelcall.PaidResult{InvocationID: invocation, Reservation: reservation}); err != nil {
		t.Fatal(err)
	}
	_, err = s.generatePaid(ctx, j, "organize", organizeInstructions, request.Prompt, nil)
	var jobErr *worker.JobError
	if !errors.As(err, &jobErr) || jobErr.Code != "provider_outcome_unknown" || jobErr.Retry || !jobErr.Until.IsZero() || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	var outcome string
	if err := s.pool.QueryRow(ctx, "SELECT outcome FROM model_calls WHERE owner_id=$1 AND execution_id=$2", string(scope.OwnerID), string(j.ID)).Scan(&outcome); err != nil || outcome != "unknown" {
		t.Fatal(outcome, err)
	}
}

func TestFailedCallAccountingRecoveryDoesNotApplyPartialOutput(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusBadGateway) })
	organizeTestMemory(t, s, scope, "Fictitious report with a failed provider.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_test_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic accounting failure'; END $$;
 CREATE TRIGGER reject_test_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_test_usage()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err == nil {
		t.Fatal("expected accounting failure")
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_test_usage ON model_usage"); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(nil)
	var jobErr *worker.JobError
	err = peer.ProcessOrganize(ctx, j)
	if !errors.As(err, &jobErr) || jobErr.Code != "model_call_failed" || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 0 {
		t.Fatal(state.Organize, err)
	}
	var callsRecorded, usageRecorded int
	if err := s.pool.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls WHERE owner_id=$1 AND outcome='failed' AND accounting_state='settled'`, string(scope.OwnerID)).Scan(&callsRecorded, &usageRecorded); err != nil || callsRecorded != 1 || usageRecorded != 1 {
		t.Fatal(callsRecorded, usageRecorded, err)
	}
}

func TestLostProviderResponseKeepsUsageAndDoesNotRepeatGeneration(t *testing.T) {
	for _, mode := range []string{"disconnect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "disconnect" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				close(entered)
				<-release
				secretaryModelReply(w, "unused")
			})
			organizeTestMemory(t, s, scope, "Fictitious report with an interrupted response.")
			j := organizeTestJob(t, s, scope)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var err error
			if mode == "cancel" {
				done := make(chan error, 1)
				go func() { done <- s.ProcessOrganize(ctx, j) }()
				<-entered
				cancel()
				close(release)
				err = <-done
			} else {
				err = s.ProcessOrganize(ctx, j)
			}
			var jobErr *worker.JobError
			if !errors.As(err, &jobErr) || jobErr.Code != "provider_outcome_unknown" {
				t.Fatal(err)
			}
			if err = s.ProcessOrganize(context.Background(), j); !errors.As(err, &jobErr) || jobErr.Code != "provider_outcome_unknown" || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
			var outcome, state string
			var usages int
			if err := s.pool.QueryRow(context.Background(), `SELECT c.outcome,c.accounting_state,(SELECT count(*) FROM model_usage u WHERE u.owner_id=c.owner_id AND u.id=c.usage_id) FROM model_calls c WHERE c.owner_id=$1 AND c.execution_id=$2`, string(scope.OwnerID), string(j.ID)).Scan(&outcome, &state, &usages); err != nil || outcome != "unknown" || state != "settled" || usages != 1 {
				t.Fatal(outcome, state, usages, err)
			}
		})
	}
}

func TestLegacyPaidResultRecoversWithoutInventedCallMetadata(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); organizeTestReply(t, w, r, "event") })
	organizeTestMemory(t, s, scope, "Fictitious report with a legacy paid receipt.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_test_application() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic application failure'; END $$;
 CREATE TRIGGER reject_test_application BEFORE UPDATE OF category ON claim_revisions FOR EACH ROW EXECUTE FUNCTION reject_test_application()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err == nil {
		t.Fatal("expected application failure")
	}
	// The persisted result has the old table shape. Remove only this fixture's
	// new metadata to represent a receipt saved before migration 062.
	if _, err := s.pool.Exec(ctx, "DELETE FROM model_calls WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_test_application ON claim_revisions"); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(nil)
	if err := peer.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 1 || calls.Load() != 1 {
		t.Fatal(state.Organize, calls.Load(), err)
	}
	var metadata, usage int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM model_calls WHERE owner_id=$1),(SELECT count(*) FROM model_usage WHERE owner_id=$1)`, string(scope.OwnerID)).Scan(&metadata, &usage); err != nil || metadata != 0 || usage != 1 {
		t.Fatal(metadata, usage, err)
	}
}
