package postgres

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// F-B2-8: exercise real calls through three phases and the real worker boundary.
// Isolating pending queues must preserve completed jobs: billing survives job
// deletion, but the hourly admission ledger needs their stage classification.
func TestOrganizeCompareHourlyPipelineRetainsCompletedHistory(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	f := b1Model(t, s, `{
 "items": [
  {
   "category": "progress",
   "deadlines": [],
   "durable": true,
   "n": 1
  }
 ],
 "new": []
}`)
	claim := func(stage string) worker.Job {
		t.Helper()
		var err error
		if stage == EntityCompareStage {
			return compareJob(t, s, scope, true, EntityCompareVersion)
		}
		if stage == OrganizeStage {
			_, err = s.ScheduleOrganize(ctx, time.Now())
		} else {
			_, err = s.ScheduleCompare(ctx, time.Now())
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE $2`, string(scope.OwnerID), stage+":%"); err != nil {
			t.Fatal(err)
		}
		job, err := s.Claim(ctx, time.Minute)
		if err != nil || job == nil || !strings.HasPrefix(job.Stage, stage+":") {
			t.Fatal("phase claim", stage, job, err)
		}
		return *job
	}
	for i := range 40 {
		organizeTestMemory(t, s, scope, fmt.Sprintf("虚构分期整理便签%03d。", i))
		if err := s.ProcessOrganize(ctx, claim(OrganizeStage)); err != nil {
			t.Fatal(err)
		}
	}
	f.set(`{"duplicates":[],"superseded":[]}`)
	for i := range 40 {
		compareFixture(t, s, scope, fmt.Sprintf("虚构分期比较便签%03d甲。", i))
		compareFixture(t, s, scope, fmt.Sprintf("虚构分期比较便签%03d乙。", i))
		if err := s.ProcessCompare(ctx, claim(CompareStage)); err != nil {
			t.Fatal(err)
		}
	}
	f.set(`{"same":false,"keep":null}`)
	for i := range 30 {
		name := fmt.Sprintf("虚构分期人物%03d", i)
		a, _ := compareEntityFixture(t, s, scope, name, "虚构人物在海岚工坊负责文具")
		b, _ := compareEntityFixture(t, s, scope, name, "虚构人物在杉木实验室负责器材")
		aliasProposalFixture(t, s, scope, a, b, EntityCompareVersion)
		if err := s.ProcessEntityCompare(ctx, claim(EntityCompareStage)); err != nil {
			t.Fatal(err)
		}
	}
	var runtime, billed, orphaned int
	if err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE j.id IS NULL) FROM background_usage b LEFT JOIN memory_jobs j ON j.id=b.job_id WHERE b.owner_id=$1`, scope.OwnerID).Scan(&billed, &orphaned); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_usage b JOIN memory_jobs j ON j.id=b.job_id WHERE b.owner_id=$1 AND (j.stage LIKE 'memory.organize:%' OR j.stage LIKE 'memory.compare:%' OR j.stage LIKE 'memory.entity_compare:%') AND b.created_at>now()-interval '1 hour'`, scope.OwnerID).Scan(&runtime); err != nil {
		t.Fatal(err)
	}
	if runtime != 110 || billed != 110 || orphaned != 0 || len(f.all()) != 110 {
		t.Fatal("phase history lost", runtime, billed, orphaned, len(f.all()))
	}
	for _, stage := range []string{CompareStage, EntityCompareStage, OrganizeStage} {
		var sentinel memory.Ref
		switch stage {
		case CompareStage:
			compareFixture(t, s, scope, "虚构额度已满比较哨兵")
		case EntityCompareStage:
			a, _ := compareEntityFixture(t, s, scope, "虚构额度已满同名", "虚构人物分别负责书籍")
			b, _ := compareEntityFixture(t, s, scope, "虚构额度已满同名", "虚构人物分别负责盆栽")
			aliasProposalFixture(t, s, scope, a, b, EntityCompareVersion)
		case OrganizeStage:
			sentinel = organizeTestMemory(t, s, scope, "虚构额度已满整理哨兵")
		}
		job := claim(stage)
		// Return the real lease to the queue, then let Worker claim/defer it.
		if err := s.Defer(ctx, job, "", time.Now(), true); err != nil {
			t.Fatal(err)
		}
		runner := worker.New(s, map[string]worker.Handler{OrganizeStage: s.ProcessOrganize, CompareStage: s.ProcessCompare, EntityCompareStage: s.ProcessEntityCompare}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if ran, err := runner.RunOnce(ctx); err != nil || !ran {
			t.Fatal("quota worker", stage, ran, err)
		}
		var state, code string
		var attempts int
		var due time.Time
		if err := s.pool.QueryRow(ctx, `SELECT state,error_code,attempts,available_at FROM memory_jobs WHERE id=$1`, job.ID).Scan(&state, &code, &attempts, &due); err != nil {
			t.Fatal(err)
		}
		want := "compare_hourly_limit"
		if stage == EntityCompareStage {
			want = "entity_compare_hourly_limit"
		}
		if stage == OrganizeStage {
			want = "organize_hourly_limit"
		}
		if state != "queued" || code != want || attempts != 0 || !due.After(time.Now()) || len(f.all()) != 110 {
			t.Fatal("worker quota deferral", stage, state, code, attempts, due, len(f.all()))
		}
		if stage == OrganizeStage {
			var organized, tries int
			if err := s.pool.QueryRow(ctx, `SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2`, scope.OwnerID, sentinel.ID).Scan(&organized, &tries); err != nil || organized != 0 || tries != 0 {
				t.Fatal("sentinel changed", organized, tries, err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE background_usage SET created_at=created_at-interval '1 hour 1 second' WHERE owner_id=$1`, scope.OwnerID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET available_at=now() WHERE id=$1`, job.ID); err != nil {
				t.Fatal(err)
			}
			f.set(`{
 "items": [
  {
   "category": "progress",
   "deadlines": [],
   "durable": true,
   "n": 1
  }
 ],
 "new": []
}`)
			if ran, err := runner.RunOnce(ctx); err != nil || !ran {
				t.Fatal("next-hour worker", ran, err)
			}
			if len(f.all()) != 111 {
				t.Fatal("next-hour call count", len(f.all()))
			}
			if err := s.pool.QueryRow(ctx, `SELECT organized FROM claims WHERE owner_id=$1 AND id=$2`, scope.OwnerID, sentinel.ID).Scan(&organized); err != nil || organized != OrganizeVersion {
				t.Fatal("next-hour sentinel", organized, err)
			}
		}
	}
}
