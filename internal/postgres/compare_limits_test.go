package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestCompareUnavailableBudgetAndSharedOrganizeLock(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	f := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "合成限额检查记忆")
	key := "PCAS_TEST_COMPARE_CHANNEL_AVAILABLE"
	t.Setenv(key, "")
	s.models.Config.Providers[0].KeyEnv = key
	j := compareJob(t, s, scope, false, CompareVersion)
	assertDeferred := func(code string) {
		t.Helper()
		err := s.ProcessCompare(ctx, j)
		var e *worker.JobError
		if !errors.As(err, &e) || e.Code != code || !e.NoAttempt || e.Until.Before(time.Now()) {
			t.Fatal(code, err)
		}
	}
	assertDeferred("provider_unavailable")
	if len(f.all()) != 0 {
		t.Fatal("unavailable called provider")
	}
	t.Setenv(key, "fictional-key")
	s.models.Config.Providers[0].InputPerMillion = 1
	if _, err := s.pool.Exec(ctx, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','0') WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	assertDeferred("budget_deferred")
	if len(f.all()) != 0 {
		t.Fatal("over-budget call")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','10') WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	locker, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended(current_database()||':'||current_schema()||':organize-call',0))"); err != nil {
		locker.Release()
		t.Fatal(err)
	}
	assertDeferred("compare_busy")
	if _, err := locker.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended(current_database()||':'||current_schema()||':organize-call',0))"); err != nil {
		t.Fatal(err)
	}
	locker.Release()
	if err := s.ProcessCompare(ctx, j); err != nil {
		t.Fatal(err)
	}
	compareState(t, s, scope, refs, []string{""}, CompareVersion)
	if len(f.all()) != 1 {
		t.Fatal("unexpected calls", len(f.all()))
	}
}
func TestCompareHourlyCapCoversOrganizeAndBothComparisonKinds(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	f := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	compareFixture(t, s, scope, "合成每小时上限记忆")
	j := compareJob(t, s, scope, false, CompareVersion)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i := 0; i < 120; i++ {
			record := j
			switch i % 3 {
			case 0:
				record.Stage = "memory.organize:1:fictional"
			case 1:
				record.Stage = "memory.compare:1:fictional"
			case 2:
				record.Stage = "memory.entity_compare:1:fictional"
			}
			var id string
			if err := tx.QueryRow(ctx, "INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state) VALUES(gen_random_uuid(),$1,$2,$3,$4,'done') ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET updated_at=now() RETURNING id::text", string(scope.OwnerID), string(j.Record.ID), j.Record.Version, record.Stage).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO background_usage(owner_id,id,job_id,reserved_cost) VALUES($1,gen_random_uuid(),$2,0)", string(scope.OwnerID), id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.ProcessCompare(ctx, j)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "compare_hourly_limit" || !failure.NoAttempt {
		t.Fatal(err)
	}
	if len(f.all()) != 0 {
		t.Fatal("hourly cap bypassed")
	}
}

func TestCompareDispatchPrecedesQueuedOrganizeBacklog(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "虚构已整理记忆", "虚构待整理记忆")
	if _, err := s.pool.Exec(ctx, "UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[1].ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleOrganize(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleCompare(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.organize:%' AND stage NOT LIKE 'memory.compare:%'", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx, 5*time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, CompareStage+":") {
		t.Fatal("organize backlog starves comparison", j, err)
	}
}

func TestCompareRuleEpochComesFromProgramNotQueueAndRemovedModelSkips(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "虚构规则纪元记忆")
	j := compareJob(t, s, scope, false, CompareVersion+7)
	if err := s.ProcessCompare(ctx, j); err != nil {
		t.Fatal(err)
	}
	compareState(t, s, scope, refs, []string{""}, CompareVersion)
	if _, err := s.pool.Exec(ctx, "UPDATE claims SET compared=0 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	j = compareJob(t, s, scope, false, CompareVersion)
	s.models = nil
	if err := s.ProcessCompare(ctx, j); err != nil {
		t.Fatal(err)
	}
	compareState(t, s, scope, refs, []string{""}, 0)
	var state string
	if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE id=$1", string(j.ID)).Scan(&state); err != nil || state != "done" {
		t.Fatal(state, err)
	}
}
