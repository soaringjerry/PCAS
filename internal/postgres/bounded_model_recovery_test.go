package postgres

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func disconnectModelResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = conn.Close()
}

func nextModelRecoveryLease(t *testing.T, s *Store, job worker.Job) worker.Job {
	t.Helper()
	ctx := context.Background()
	if err := s.Retry(ctx, job, modelcall.ErrOutcomeUnknown.Error()); err != nil {
		t.Fatal(err)
	}
	var delayed bool
	if err := s.pool.QueryRow(ctx, `SELECT available_at>clock_timestamp() FROM memory_jobs WHERE id=$1`, string(job.ID)).Scan(&delayed); err != nil || !delayed {
		t.Fatal("recovery must use queue backoff", delayed, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET available_at=clock_timestamp()-interval '1 second' WHERE id=$1`, string(job.ID)); err != nil {
		t.Fatal(err)
	}
	next, err := s.Claim(ctx, time.Minute)
	if err != nil || next == nil || next.ID != job.ID || next.LeaseToken == job.LeaseToken {
		t.Fatal("recovery must use a fresh lease", next, err)
	}
	return *next
}

func requireRecoveryFailure(t *testing.T, err error, code string, retry bool) {
	t.Helper()
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != code || failure.Retry != retry || !failure.Until.IsZero() {
		t.Fatal("unexpected recovery decision", err)
	}
}

func TestMeteredExtractionRecoversLostResponsesWithSeparateCalls(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	text := "虚构测试：我下周去成都见老王。"
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			disconnectModelResponse(t, w)
			return
		}
		secretaryModelReply(w, map[string]any{"items": []any{b2Item(text, text, "plan")}})
	})
	s.models.Config.Providers[0].CostMode = ""
	s.models.Config.Providers[0].InputPerMillion = 1
	s.models.Config.Providers[0].OutputPerMillion = 1
	src := b2Source(t, s, scope, "desk", "user", text, nil)
	b2OnlyExtraction(t, s, scope, src, 0)
	j, err := s.Claim(context.Background(), time.Minute)
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	for range 2 {
		requireRecoveryFailure(t, s.ProcessExtraction(context.Background(), *j), modelcall.ErrOutcomeUnknown.Error(), true)
		state := b2Snapshot(t, s, scope)
		if len(state.Memories) != 0 {
			t.Fatal("unknown output must not create memories")
		}
		next := nextModelRecoveryLease(t, s, *j)
		j = &next
	}
	if err := s.ProcessExtraction(context.Background(), *j); err != nil {
		t.Fatal(err)
	}
	var unknown, returned, links, usage int
	var held float64
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FILTER(WHERE c.outcome='unknown' AND c.recovery_state='replaced'),count(*) FILTER(WHERE c.outcome='returned'),count(c.retry_of_id),(SELECT count(*) FROM model_usage WHERE owner_id=$1),coalesce(sum(b.reserved_cost) FILTER(WHERE c.outcome='unknown'),0) FROM model_calls c JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1`, string(scope.OwnerID)).Scan(&unknown, &returned, &links, &usage, &held); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || unknown != 2 || returned != 1 || links != 2 || usage != 3 || held <= 0 || len(b2Snapshot(t, s, scope).Memories) != 1 {
		t.Fatal(calls.Load(), unknown, returned, links, usage, held)
	}
}

func TestPersistentStagePausesAtRecordedRecoveryLimitAfterRestart(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	var allowReply atomic.Bool
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if allowReply.Load() {
			organizeTestReply(t, w, r, "event")
			return
		}
		disconnectModelResponse(t, w)
	})
	s.models.Config.Providers[0].CostMode = ""
	s.models.Config.Providers[0].InputPerMillion = 1
	s.models.Config.Providers[0].OutputPerMillion = 1
	ref := organizeTestMemory(t, s, scope, "Fictitious report interrupted by network failure.")
	before, err := s.GetMemory(context.Background(), scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	j := organizeTestJob(t, s, scope)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		peer, err := Open(context.Background(), s.pool.Config().ConnConfig.ConnString())
		if err != nil {
			t.Fatal(err)
		}
		peer.SetModels(s.models)
		err = peer.ProcessOrganize(context.Background(), j)
		peer.Close()
		if attempt == maxAttempts {
			requireRecoveryFailure(t, err, modelcall.ErrRetryExhausted.Error(), false)
			break
		}
		requireRecoveryFailure(t, err, modelcall.ErrOutcomeUnknown.Error(), true)
		j = nextModelRecoveryLease(t, s, j)
	}
	if err := s.Block(context.Background(), j, modelcall.ErrRetryExhausted.Error()); err != nil {
		t.Fatal(err)
	}
	var count, lastAttempt int
	var state, reason, jobState, jobReason string
	if err := s.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM model_calls WHERE owner_id=$1),c.attempt_number,c.recovery_state,c.recovery_reason,j.state,j.error_code FROM model_calls c JOIN memory_jobs j ON j.id=c.execution_id WHERE c.owner_id=$1 AND c.recovery_state='exhausted'`, string(scope.OwnerID)).Scan(&count, &lastAttempt, &state, &reason, &jobState, &jobReason); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMemory(context.Background(), scope, string(ref.ID))
	if err != nil || m.Category != before.Category || m.Text != before.Text || m.Version != before.Version || calls.Load() != int32(maxAttempts) || count != maxAttempts || lastAttempt != maxAttempts || state != "exhausted" || reason != modelcall.ErrRetryExhausted.Error() || jobState != "blocked" || jobReason != reason {
		t.Fatal(m, err, calls.Load(), count, lastAttempt, state, reason, jobState, jobReason)
	}
	if _, err := s.Claim(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != int32(maxAttempts) {
		t.Fatal("blocked recovery must not call again")
	}
	// The existing owner retry control grants another bounded allowance.
	// It keeps all old calls, the root, and the explicit authorization.
	allowReply.Store(true)
	workspaceCommand(t, s, scope, workspace.Command{Type: "retryJob", ID: string(j.ID)})
	retried, err := s.Claim(context.Background(), time.Minute)
	if err != nil || retried == nil || retried.ID != j.ID || retried.Attempts != 1 {
		t.Fatal(retried, err)
	}
	if err := s.ProcessOrganize(context.Background(), *retried); err != nil {
		t.Fatal(err)
	}
	var authorization, oldReason string
	var newAttempt, oldAttempt int
	if err := s.pool.QueryRow(context.Background(), `SELECT next.attempt_number,prior.attempt_number,prior.actual_mode->>'recoveryRequestId',prior.actual_mode->>'previousRecoveryReason' FROM model_calls next JOIN model_calls prior ON prior.owner_id=next.owner_id AND prior.id=next.retry_of_id WHERE next.owner_id=$1 AND next.execution_id=$2 AND next.outcome='returned'`, string(scope.OwnerID), string(j.ID)).Scan(&newAttempt, &oldAttempt, &authorization, &oldReason); err != nil || newAttempt != 1 || oldAttempt != maxAttempts || authorization == "" || oldReason != modelcall.ErrRetryExhausted.Error() || calls.Load() != int32(maxAttempts+1) {
		t.Fatal(newAttempt, oldAttempt, authorization, oldReason, calls.Load(), err)
	}
}

func TestUnknownRecoveryPausesWhenBudgetCannotCoverReplacement(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		disconnectModelResponse(t, w)
	})
	s.models.Config.Providers[0].CostMode = ""
	s.models.Config.Providers[0].InputPerMillion = 1
	s.models.Config.Providers[0].OutputPerMillion = 1
	organizeTestMemory(t, s, scope, "Fictitious report with a limited budget.")
	j := organizeTestJob(t, s, scope)
	requireRecoveryFailure(t, s.ProcessOrganize(context.Background(), j), modelcall.ErrOutcomeUnknown.Error(), true)
	if _, err := s.pool.Exec(context.Background(), `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','0') WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	j = nextModelRecoveryLease(t, s, j)
	requireRecoveryFailure(t, s.ProcessOrganize(context.Background(), j), modelcall.ErrRecoveryBudgetExhausted.Error(), false)
	var state, reason string
	var held float64
	if err := s.pool.QueryRow(context.Background(), `SELECT c.recovery_state,c.recovery_reason,(SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1) FROM model_calls c WHERE owner_id=$1 AND recovery_state='budget_exhausted'`, string(scope.OwnerID)).Scan(&state, &reason, &held); err != nil || reason != "budget_deferred" || held <= 0 || calls.Load() != 1 {
		t.Fatal(state, reason, held, calls.Load(), err)
	}
	requireRecoveryFailure(t, s.ProcessOrganize(context.Background(), j), modelcall.ErrRecoveryBudgetExhausted.Error(), false)
	if calls.Load() != 1 {
		t.Fatal("budget pause must not silently become a replacement")
	}
}

func TestReservationWriteFailureDoesNotResetUnknownRecoveryCounter(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			disconnectModelResponse(t, w)
			return
		}
		organizeTestReply(t, w, r, "event")
	})
	s.models.Config.Providers[0].CostMode = ""
	s.models.Config.Providers[0].InputPerMillion = 1
	s.models.Config.Providers[0].OutputPerMillion = 1
	organizeTestMemory(t, s, scope, "Fictitious report with an interrupted reservation write.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	requireRecoveryFailure(t, s.ProcessOrganize(ctx, j), modelcall.ErrOutcomeUnknown.Error(), true)
	j = nextModelRecoveryLease(t, s, j)
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_test_reservation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic reservation failure'; END $$;
 CREATE TRIGGER reject_test_reservation BEFORE INSERT ON background_usage FOR EACH ROW EXECUTE FUNCTION reject_test_reservation()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err == nil || calls.Load() != 1 {
		t.Fatal("failed reservation must not invoke the provider", err, calls.Load())
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_test_reservation ON background_usage`); err != nil {
		t.Fatal(err)
	}
	j = nextModelRecoveryLease(t, s, j)
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	var attempt, failedAttempt int
	if err := s.pool.QueryRow(ctx, `SELECT next.attempt_number,prior.attempt_number FROM model_calls next JOIN model_calls prior ON prior.owner_id=next.owner_id AND prior.id=next.retry_of_id WHERE next.owner_id=$1 AND next.outcome='returned' AND prior.error_code='reservation_failed'`, string(scope.OwnerID)).Scan(&attempt, &failedAttempt); err != nil || attempt != 2 || failedAttempt != 2 || calls.Load() != 2 {
		t.Fatal("reservation retry lost the original counter", attempt, failedAttempt, calls.Load(), err)
	}
}

func TestLateInterruptedCallCannotDeleteReplacementPaidResult(t *testing.T) {
	s, scope := testStore(t), owner()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			disconnectModelResponse(t, w)
			return
		}
		organizeTestReply(t, w, r, "event")
	})
	ref := organizeTestMemory(t, s, scope, "Fictitious report with a late interrupted response.")
	j := organizeTestJob(t, s, scope)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_test_application() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic application failure'; END $$;
 CREATE TRIGGER reject_test_application BEFORE UPDATE OF category ON claim_revisions FOR EACH ROW EXECUTE FUNCTION reject_test_application()`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.generatePaid(ctx, j, "organize", organizeInstructions, asJSON(map[string]any{"fictitious": true}), nil)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("original call did not start")
	}
	// Cleanup releases only this fixture's provider, even if a later check fails.
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	if _, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, string(j.ID)); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.Claim(ctx, time.Minute)
	if err != nil || replacement == nil || replacement.ID != j.ID {
		t.Fatal(replacement, err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(s.models)
	if err := peer.ProcessOrganize(ctx, *replacement); err == nil {
		t.Fatal("expected isolated business-write failure")
	}
	close(release)
	if err := <-done; !errors.Is(err, worker.ErrLeaseLost) {
		t.Fatal("the old worker must remain fenced", err)
	}
	var receipts int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE owner_id=$1 AND job_id=$2`, string(scope.OwnerID), string(j.ID)).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal("late failure removed the replacement result", receipts, err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_test_application ON claim_revisions`); err != nil {
		t.Fatal(err)
	}
	if err := peer.ProcessOrganize(ctx, *replacement); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMemory(ctx, scope, string(ref.ID))
	if err != nil || m.Category != "event" || calls.Load() != 2 {
		t.Fatal(m, err, calls.Load())
	}
}
