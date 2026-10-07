package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestPhase26A3ClassificationClaimsBeforeAllOtherBackgroundStages(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	// Forty already classified rows supply comparison/handover inputs; the other
	// 4960 rows still need classification. No quotas or paid history are cleared.
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1 AND id=ANY($2::uuid[])`, f.Scope.OwnerID, f.Claims[:40])
	phase26Proposal(t, f, 0, 4)
	for _, stage := range []string{CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage, OrganizeStage} {
		if _, err := phase26Schedule(f.Context, f.Store, stage); err != nil {
			t.Fatal(err)
		}
	}
	// Input ingestion/indexing is an external prerequisite, not one of the five
	// stage budgets being ordered here. Leave their rows intact but not due.
	phase26Exec(t, f, `UPDATE memory_jobs SET available_at=now()+interval '1 day' WHERE state='queued' AND NOT(stage LIKE 'memory.organize:%' OR stage LIKE 'memory.compare:%' OR stage LIKE 'memory.entity_compare:%' OR stage LIKE 'memory.entity_candidates:%' OR stage LIKE 'memory.handover:%')`)
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage} {
		var pending int
		if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE state='queued' AND stage LIKE $1 AND available_at<=now()`, stage+":%").Scan(&pending); err != nil || pending == 0 {
			t.Fatalf("stage %s not simultaneously eligible pending=%d err=%v", stage, pending, err)
		}
	}
	j, err := f.Store.Claim(f.Context, time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, OrganizeStage+":") {
		t.Fatalf("classification not dispatched first among all five due stages: %+v %v", j, err)
	}
	if err := f.Store.ProcessOrganize(f.Context, *j); err != nil {
		t.Fatal(err)
	}
	if len(m.calls(OrganizeStage)) != 1 || len(m.calls(CompareStage))+len(m.calls(EntityCompareStage))+len(m.calls(EntityCandidatesStage))+len(m.calls(HandoverStage)) != 0 {
		t.Fatal("classification priority made unrelated provider calls")
	}
	t.Log("all five background stages simultaneously due; actual Claim dispatches classification first and executes one classification call")
}
