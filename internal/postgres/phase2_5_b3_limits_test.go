package postgres_test

import (
	"testing"
	"time"
)

// All 200 card jobs must be claimed: 120 complete, 80 defer. Leave room
// for a handover job and the final empty claim in either simulated hour.

// Seed cards after all inputs exist, avoiding repeated invalidation of a
// growing set of test cards while constructing the initial fictitious graph.

// Advance the owned database one hour relative to the handlers' wall clock.
// All real ledger rows remain present, and deferred jobs become due.

// Give this fictitious owner a timezone whose current local day has at
// least twelve elapsed hours. Two six-hour clock advances then keep all
// three actual handover calls in one user calendar day, even in midnight CI.

// Model and queue rows were produced by real calls. Shift them consistently;
// do not delete or counterfeit reservations/usage to bypass a limit.
func (f *phase25B234Fixture) advanceStatusTime(t *testing.T, elapsed time.Duration) {
	t.Helper()
	for _, tc := range []struct{ table, column string }{{"model_usage", "at"}, {"background_usage", "created_at"}, {"status_cards", "built_at"}, {"handovers", "built_at"}, {"memory_records", "updated_at"}, {"record_versions", "recorded_at"}, {"memory_jobs", "available_at"}, {"memory_jobs", "created_at"}, {"memory_jobs", "updated_at"}} {
		f.exec(t, "UPDATE "+tc.table+" SET "+tc.column+"="+tc.column+"-$2::double precision*interval '1 second' WHERE owner_id=$1", f.scope.OwnerID, elapsed.Seconds())
	}
}

func TestPhase25B3_NoModelDoesNotSchedule(t *testing.T) {
	f := phase25B234NewFixture(t)
	f.cardGroup(t, 3)
	if n := f.scheduleStatus(t, time.Now().Add(11*time.Minute)); n != 0 {
		t.Errorf("unconfigured model scheduled=%d", n)
	}
}

// The captured prompt shape is deliberately centralized; this test only
// verifies that every delivered current Memory carries the contract trust.
