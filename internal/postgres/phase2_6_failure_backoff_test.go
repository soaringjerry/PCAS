package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestPhase26G2A5ComparisonInvalidOutputKeepsSameBatchPending(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26LibraryPrecondition(t, f)
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	m.mu.Lock()
	m.Reply = func(phase26Call) string { return `{"fictitious_invalid":true}` }
	m.mu.Unlock()
	before := phase26Digest(t, f, []string{"claim_revisions", "deadlines", "assistant_requirements"})
	var previous time.Duration
	for i := 1; i <= 4; i++ {
		// Advance only this batch's retry clock after its real persisted backoff has
		// been measured; all other receipts and retained usage remain intact.
		phase26Exec(t, f, `UPDATE memory_comparison_batches SET retry_after='-infinity' WHERE owner_id=$1 AND attempts>0`, f.Scope.OwnerID)
		phase26Isolate(t, f, CompareStage)
		if _, err := f.Store.ScheduleCompare(f.Context, time.Now()); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, CompareStage)
		j := phase26ClaimStage(t, f, CompareStage)
		if err := f.Store.ProcessCompare(f.Context, j); err != nil {
			t.Fatal(err)
		}
		var attempts, completed int
		var seconds float64
		if err := f.Store.pool.QueryRow(f.Context, `SELECT attempts,extract(epoch from retry_after-clock_timestamp()),(completed_at IS NOT NULL)::int FROM memory_comparison_batches WHERE owner_id=$1 AND attempts>0 ORDER BY attempts DESC LIMIT 1`, f.Scope.OwnerID).Scan(&attempts, &seconds, &completed); err != nil {
			t.Fatal(err)
		}
		delay := time.Duration(seconds * float64(time.Second))
		if attempts != i || completed != 0 || delay <= previous {
			t.Fatalf("invalid retry attempt=%d completed=%d delay=%s previous=%s", attempts, completed, delay, previous)
		}
		previous = delay
		t.Logf("invalid comparison actual_provider_attempt=%d persisted_retry_delay=%s completed=false", i, delay)
	}
	after := phase26Digest(t, f, []string{"claim_revisions", "deadlines", "assistant_requirements"})
	for table, hash := range before {
		if after[table] != hash {
			t.Errorf("invalid comparison altered good %s", table)
		}
	}
	if len(m.calls(CompareStage)) != 4 {
		t.Errorf("calls=%d", len(m.calls(CompareStage)))
	}
	var complete int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_group_progress WHERE owner_id=$1 AND group_key='self:rule' AND completed`, f.Scope.OwnerID).Scan(&complete); err != nil {
		t.Fatal(err)
	}
	if complete != 0 {
		t.Errorf("failed self:rule batch marked %d memories complete", complete)
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(health), "compare_invalid_output") {
		t.Errorf("missing concrete invalid output reason: %s", health)
	}
	t.Logf("health=%s", health)
	phase26InvalidHealth(t, health, CompareStage)
}
