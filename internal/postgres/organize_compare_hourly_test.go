package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"testing"
	"time"
)

func seedStageUsage(t *testing.T, s *Store, scope memory.Scope, stage string, count int) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `INSERT INTO background_usage(owner_id,stage,reserved_cost,created_at) SELECT $1,$2,0,now()-interval '10 minutes' FROM generate_series(1,$3)`, scope.OwnerID, stage, count); err != nil {
		t.Fatal(err)
	}
}

// Every processor uses its own rolling budget; saturation in another stage
// neither prevents this stage's last call nor lends it a forty-first call.
func TestOrganizeCompareIndependentHourlyCalls(t *testing.T) {
	for _, kind := range []string{OrganizeStage, CompareStage, EntityCompareStage} {
		t.Run(kind, func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			fake := b1Model(t, s, `{"items":[]}`)
			var job worker.Job
			var process func(context.Context, worker.Job) error
			switch kind {
			case OrganizeStage:
				organizeTestMemory(t, s, scope, "虚构独立额度整理便签")
				job = organizeTestJob(t, s, scope)
				process = s.ProcessOrganize
			case CompareStage:
				fake.set(`{"duplicates":[],"superseded":[]}`)
				compareFixture(t, s, scope, "虚构独立额度比较便签甲", "虚构独立额度比较便签乙")
				job = compareJob(t, s, scope, false, CompareVersion)
				process = s.ProcessCompare
			case EntityCompareStage:
				fake.set(`{"same":false,"keep":null}`)
				_, a := compareEntityFixture(t, s, scope, "虚构额度同名", "虚构人物甲")
				_, b := compareEntityFixture(t, s, scope, "虚构额度同名", "虚构人物乙")
				compareEntityGroup(t, s, scope, "project", "虚构额度项目", a, b)
				job = compareJob(t, s, scope, true, CompareVersion)
				process = s.ProcessEntityCompare
			}
			for stage, limit := range map[string]int{OrganizeStage: 40, CompareStage: 40, EntityCompareStage: 30, EntityCandidatesStage: 6, HandoverStage: 2} {
				if stage == kind {
					limit--
				}
				seedStageUsage(t, s, scope, stage, limit)
			}
			if err := process(ctx, job); err != nil {
				t.Fatalf("last stage slot rejected: %v", err)
			}
			if len(fake.all()) != 1 {
				t.Fatal("expected one model call", len(fake.all()))
			}
			switch kind {
			case OrganizeStage:
				organizeTestMemory(t, s, scope, "虚构新的整理便签")
			case CompareStage:
				compareFixture(t, s, scope, "虚构新的比较便签")
			case EntityCompareStage:
				entity, a := compareEntityFixture(t, s, scope, "虚构额度同名", "虚构人物丙")
				var existing memory.Ref
				existing.Kind = memory.EntityKind
				if err := s.pool.QueryRow(ctx, "SELECT id::text,version FROM memory_records WHERE owner_id=$1 AND kind='entity' AND id<>$2 AND state='active' AND EXISTS(SELECT 1 FROM entity_versions v WHERE(v.owner_id,v.entity_id,v.version)=(memory_records.owner_id,memory_records.id,memory_records.version) AND v.entity_type='person') ORDER BY id LIMIT 1", scope.OwnerID, entity.ID).Scan(&existing.ID, &existing.Version); err != nil {
					t.Fatal(err)
				}
				aliasProposalFixture(t, s, scope, existing, entity, EntityCompareVersion)
				compareEntityGroup(t, s, scope, "project", "虚构额度项目", a)
			}
			job = leaseStage(t, s, scope, job.Record, fmt.Sprintf("%s:1:%s", kind, memory.NewID()))
			var before int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_usage").Scan(&before); err != nil {
				t.Fatal(err)
			}
			err := process(ctx, job)
			var failure *worker.JobError
			code := "compare_hourly_limit"
			if kind == EntityCompareStage {
				code = "entity_compare_hourly_limit"
			}
			if kind == OrganizeStage {
				code = "organize_hourly_limit"
			}
			if !errors.As(err, &failure) || failure.Code != code || !failure.NoAttempt || failure.Until.Before(time.Now()) {
				t.Fatal("own quota must defer", err)
			}
			var after int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_usage").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if len(fake.all()) != 1 || before != after {
				t.Fatal("deferred call sent or reserved", len(fake.all()), before, after)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE background_usage SET created_at=now()-interval '2 hours' WHERE id=(SELECT id FROM background_usage WHERE stage=$1 AND created_at>now()-interval '1 hour' ORDER BY created_at,id LIMIT 1)`, kind); err != nil {
				t.Fatal(err)
			}
			if err := process(ctx, job); err != nil {
				t.Fatal("own released slot blocked", err)
			}
			if len(fake.all()) != 2 {
				t.Fatal("released slot must call once", len(fake.all()))
			}
		})
	}
}
