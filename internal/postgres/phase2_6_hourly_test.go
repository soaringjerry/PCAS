package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestPhase26G7T4ActualProviderAttemptAtEachHourlyBoundary(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	s, ctx, owner := f.Store, f.Context, f.Scope.OwnerID
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, owner)
	phase26Exec(t, f, `UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=$2`, owner, f.Claims[4998])
	phase26Proposal(t, f, 0, 4)
	stages := []struct {
		stage string
		limit int
	}{{OrganizeStage, 40}, {CompareStage, 40}, {EntityCompareStage, 30}, {EntityCandidatesStage, 6}, {HandoverStage, 2}}
	// Retained prior-hour history is an explicit precondition. Exactly FIVE
	// attempts are made by the real provider, each consuming its final free slot.
	for _, tc := range stages {
		phase26Exec(t, f, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,$3)`, owner, tc.stage, tc.limit-1)
	}
	for _, tc := range stages {
		phase26Isolate(t, f, tc.stage)
		if _, err := phase26Schedule(ctx, s, tc.stage); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, tc.stage)
		j := phase26ClaimStage(t, f, tc.stage)
		if err := phase26Process(ctx, s, j); err != nil {
			t.Fatalf("last available %s slot: %v", tc.stage, err)
		}
		if len(m.calls(tc.stage)) != 1 {
			t.Errorf("final %s slot actual attempts=%d", tc.stage, len(m.calls(tc.stage)))
		}
		var retained int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_usage WHERE stage=$1 AND created_at>now()-interval '1 hour'`, tc.stage).Scan(&retained); err != nil || retained != tc.limit {
			t.Fatalf("retained %s hourly ledger=%d expected=%d err=%v", tc.stage, retained, tc.limit, err)
		}
	}
	phase26Exec(t, f, `UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=$2`, owner, f.Claims[4999])
	phase26Proposal(t, f, 8, 12)
	phase26Exec(t, f, `UPDATE handovers SET built_at=now()-interval '61 minutes',input_hash='' WHERE owner_id=$1`, owner)
	for _, tc := range stages {
		phase26Isolate(t, f, tc.stage)
		if _, err := phase26Schedule(ctx, s, tc.stage); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, tc.stage)
		j := phase26ClaimStage(t, f, tc.stage)
		e := phase26FinishError(t, f, j, phase26Process(ctx, s, j))
		if !e.NoAttempt || !strings.HasSuffix(e.Code, "hourly_limit") {
			t.Errorf("%s borrowed spare global calls or consumed attempt: %+v", tc.stage, e)
		}
		if len(m.calls(tc.stage)) != 1 {
			t.Errorf("hourly %s guard called provider again", tc.stage)
		}
	}
	health, err := s.BackgroundHealth(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("five actual provider attempts; independent retained hourly ceilings=40/40/30/6/2; health=%s; clock=%s", health, time.Now().UTC().Format(time.RFC3339))
}
