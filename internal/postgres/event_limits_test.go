package postgres

import (
	"context"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// The index of migration 064 and the statement of the limit hold the number
// of ChainDepthLimit. A change of one without the others would make the
// statement scan the queue.
func TestChainLimitNumberIsTheSameInCodeStatementAndIndex(t *testing.T) {
	migration, err := migrations.ReadFile("migrations/064_event_limits.sql")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`chain_depth > (\d+)`).FindSubmatch(migration)
	if found == nil || string(found[1]) != strconv.Itoa(ChainDepthLimit) {
		t.Fatal("the index of migration 064 does not hold ChainDepthLimit", string(migration))
	}
}

func TestJobOfAChainAboveTheLimitIsStoppedVisiblyAndCanBeFoundAgain(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	source := mustIngest(t, s, scope, input())
	if _, err := s.Pool.Exec(ctx, "UPDATE memory_jobs SET state='done' WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	insert := func(stage string, depth int) string {
		id := string(memory.NewID())
		if _, err := s.Pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES($1,$2,$3,$4,$5)`, id, string(scope.OwnerID), string(source.ID), source.Version, stage); err != nil {
			t.Fatal(err)
		}
		// The trigger gives a new job its depth from its cause. The test sets the depth of a long chain directly.
		if _, err := s.Pool.Exec(ctx, "UPDATE memory_jobs SET chain_depth=$2 WHERE id=$1", id, depth); err != nil {
			t.Fatal(err)
		}
		return id
	}
	atLimit, above := insert("source.tokenize:fictitious-a", ChainDepthLimit), insert("source.tokenize:fictitious-b", ChainDepthLimit+1)
	job, err := s.Claim(ctx, time.Minute)
	if err != nil || job == nil || string(job.ID) != atLimit {
		t.Fatal("a job at the limit was not run", job, err)
	}
	var state, code string
	if err := s.Pool.QueryRow(ctx, "SELECT state,error_code FROM memory_jobs WHERE id=$1", above).Scan(&state, &code); err != nil || state != "failed" || code != "chain_limit" {
		t.Fatal("a job above the limit was not stopped", state, code, err)
	}
	var count int
	if err := s.Pool.QueryRow(ctx, "SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND stage='source.tokenize' AND outcome='failure' AND reason='chain_limit'", string(scope.OwnerID)).Scan(&count); err != nil || count != 1 {
		t.Fatal("the stopped job was not counted once", count, err)
	}
	// A second pass counts nothing again, and the job keeps its row, its root and its depth.
	if next, err := s.Claim(ctx, time.Minute); err != nil || next != nil {
		t.Fatal(next, err)
	}
	var depth int
	if err := s.Pool.QueryRow(ctx, "SELECT chain_depth,(SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$2 AND reason='chain_limit') FROM memory_jobs WHERE id=$1", above, string(scope.OwnerID)).Scan(&depth, &count); err != nil || depth != ChainDepthLimit+1 || count != 1 {
		t.Fatal(depth, count, err)
	}
}

func TestOldEventsOfAFinishedChainAreRemovedAndThoseOfAWaitingChainAreKept(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	source := mustIngest(t, s, scope, input())
	if _, err := s.Pool.Exec(ctx, "UPDATE memory_jobs SET state='done' WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	waiting := string(memory.NewID())
	if _, err := s.Pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,available_at) VALUES($1,$2,$3,$4,'source.tokenize:fictitious-waiting',now()+interval '1 day')`, waiting, string(scope.OwnerID), string(source.ID), source.Version); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM recorded_events WHERE owner_id=$1", string(scope.OwnerID)).Scan(&before); err != nil || before < 2 {
		t.Fatal("the fixture recorded no events", before, err)
	}
	old := time.Now().Add(-EventRetention - time.Hour)
	if _, err := s.Pool.Exec(ctx, "UPDATE recorded_events SET occurred_at=$2 WHERE owner_id=$1", string(scope.OwnerID), old); err != nil {
		t.Fatal(err)
	}
	lastEventRetention.Store(0)
	if err := s.applyEventLimits(ctx); err != nil {
		t.Fatal(err)
	}
	var kept, keptWaiting int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER (WHERE job_id=$2) FROM recorded_events WHERE owner_id=$1", string(scope.OwnerID), waiting).Scan(&kept, &keptWaiting); err != nil || kept != 1 || keptWaiting != 1 {
		t.Fatal("retention kept", kept, "events, of the waiting chain", keptWaiting, err)
	}
	// Within the hour the retention does not run again.
	if _, err := s.Pool.Exec(ctx, "UPDATE memory_jobs SET state='done' WHERE id=$1", waiting); err != nil {
		t.Fatal(err)
	}
	if err := s.applyEventLimits(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM recorded_events WHERE owner_id=$1", string(scope.OwnerID)).Scan(&kept); err != nil || kept != 1 {
		t.Fatal(kept, err)
	}
}
