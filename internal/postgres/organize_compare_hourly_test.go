package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// The three public processors must admit the 120th combined invocation and
// defer the 121st, without counting unrelated work or advancing retry attempts.
func TestOrganizeCompareShareHourlyCalls(t *testing.T) {
	for _, kind := range []string{OrganizeStage, CompareStage, EntityCompareStage} {
		t.Run(kind, func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			f := b1Model(t, s, `{"items":[]}`)
			var job worker.Job
			var process func(context.Context, worker.Job) error
			switch kind {
			case OrganizeStage:
				organizeTestMemory(t, s, scope, "虚构共同额度整理便签")
				job = organizeTestJob(t, s, scope)
				process = s.ProcessOrganize
			case CompareStage:
				f.set(`{"duplicates":[],"superseded":[]}`)
				compareFixture(t, s, scope, "虚构共同额度比较便签")
				job = compareJob(t, s, scope, false, CompareVersion)
				process = s.ProcessCompare
			case EntityCompareStage:
				f.set(`{"same":false,"keep":null}`)
				_, a := compareEntityFixture(t, s, scope, "虚构共同额度同名", "虚构共同额度人物甲")
				_, b := compareEntityFixture(t, s, scope, "虚构共同额度同名", "虚构共同额度人物乙")
				compareEntityGroup(t, s, scope, "project", "虚构共同额度项目", a, b)
				job = compareJob(t, s, scope, true, CompareVersion)
				process = s.ProcessEntityCompare
			}
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				stages := []string{OrganizeStage, CompareStage, EntityCompareStage}
				for i, stage := range append(stages, "source.extract", "memory.card", "memory.handover", "memory.organize_other") {
					var id string
					if err := tx.QueryRow(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state) VALUES(gen_random_uuid(),$1,$2,$3,$4,'done') RETURNING id::text`, string(scope.OwnerID), string(job.Record.ID), job.Record.Version, stage+":1:fictional-hourly").Scan(&id); err != nil {
						return err
					}
					calls := 40
					if i == 2 {
						calls = 39
					}
					for range calls {
						// One reservation per invocation, including retries of the same job;
						// no model_usage row is required for an already-issued failed call.
						if _, err := tx.Exec(ctx, `INSERT INTO background_usage(owner_id,job_id,reserved_cost,created_at) VALUES($1,$2,0,now()-interval '10 minutes')`, string(scope.OwnerID), id); err != nil {
							return err
						}
					}
					if _, err := tx.Exec(ctx, `INSERT INTO background_usage(owner_id,job_id,reserved_cost,created_at) VALUES($1,$2,0,now()-interval '2 hours')`, string(scope.OwnerID), id); err != nil {
						return err
					}
				}
				_, err := tx.Exec(ctx, `INSERT INTO background_usage(owner_id,reserved_cost) VALUES($1,0)`, string(scope.OwnerID))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = process(ctx, job); err != nil {
				t.Fatalf("120th combined call rejected: %v", err)
			}
			if len(f.all()) != 1 {
				t.Fatal("120th call not sent exactly once", len(f.all()))
			}
			// Supply pending work again without removing the historical usage jobs.
			if kind == CompareStage {
				compareFixture(t, s, scope, "虚构共同额度下一条比较便签")
			} else if kind == EntityCompareStage {
				_, a := compareEntityFixture(t, s, scope, "虚构共同额度同名", "虚构共同额度人物丙")
				compareEntityGroup(t, s, scope, "project", "虚构共同额度项目", a)
			}
			job = leaseStage(t, s, scope, job.Record, fmt.Sprintf("%s:1:%s", kind, memory.NewID()))
			var reservations, attempts int
			var earliest time.Time
			if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM background_usage`).Scan(&reservations); err != nil {
				t.Fatal(err)
			}
			if err = s.pool.QueryRow(ctx, `SELECT coalesce(sum(organize_attempts),0) FROM claims WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if err = s.pool.QueryRow(ctx, `SELECT min(b.created_at) FROM background_usage b JOIN memory_jobs j ON j.id=b.job_id WHERE (j.stage LIKE 'memory.organize:%' OR j.stage LIKE 'memory.compare:%' OR j.stage LIKE 'memory.entity_compare:%') AND b.created_at>now()-interval '1 hour'`).Scan(&earliest); err != nil {
				t.Fatal(err)
			}
			err = process(ctx, job)
			code := "compare_hourly_limit"
			if kind == OrganizeStage {
				code = "organize_hourly_limit"
			}
			var failure *worker.JobError
			if !errors.As(err, &failure) || failure.Code != code || !failure.NoAttempt || !failure.Until.Equal(earliest.Add(time.Hour+time.Second)) {
				t.Fatalf("121st call not deferred correctly: %v", err)
			}
			var afterReservations, afterAttempts int
			if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM background_usage`).Scan(&afterReservations); err != nil {
				t.Fatal(err)
			}
			if err = s.pool.QueryRow(ctx, `SELECT coalesce(sum(organize_attempts),0) FROM claims WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&afterAttempts); err != nil {
				t.Fatal(err)
			}
			if len(f.all()) != 1 || afterReservations != reservations || afterAttempts != attempts {
				t.Fatal("deferral issued a call, reserved cost or advanced attempts", len(f.all()), afterReservations, afterAttempts)
			}
			// Once the oldest counted invocation leaves the rolling hour, the very
			// same deferred job can use the released slot.
			if _, err = s.pool.Exec(ctx, `UPDATE background_usage SET created_at=now()-interval '2 hours' WHERE id=(SELECT b.id FROM background_usage b JOIN memory_jobs j ON j.id=b.job_id WHERE j.stage LIKE 'memory.organize:%' AND b.created_at>now()-interval '1 hour' ORDER BY b.created_at,b.id LIMIT 1)`); err != nil {
				t.Fatal(err)
			}
			if err = process(ctx, job); err != nil {
				t.Fatal("released slot remained blocked", err)
			}
			if len(f.all()) != 2 {
				t.Fatal("released slot did not issue exactly one call", len(f.all()))
			}
		})
	}
}
