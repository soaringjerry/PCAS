package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"net/http"
	"testing"

	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestPaidBackgroundResultsSurviveBusinessWriteFailureAndRestart(t *testing.T) {
	for _, stage := range []string{CompareStage, EntityCompareStage, EntityCandidatesStage} {
		t.Run(stage, func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			fake := b1Model(t, s, `{}`)
			var job worker.Job
			var process func(*Store, context.Context, worker.Job) error
			table := ""
			response := ""
			switch stage {
			case CompareStage:
				compareFixture(t, s, scope, "虚构研究记录甲", "虚构研究记录乙")
				job = compareJob(t, s, scope, false, CompareVersion)
				process = (*Store).ProcessCompare
				table = "memory_comparison_batches"
				response = `{"duplicates":[],"superseded":[]}`
			case EntityCompareStage:
				a, _ := compareEntityFixture(t, s, scope, "Al", "虚构阿蓝负责器材")
				b, _ := compareEntityFixture(t, s, scope, "虚构齐鹤", "虚构齐鹤负责盆栽")
				aliasProposalFixture(t, s, scope, a, b, EntityCompareVersion)
				job = compareJob(t, s, scope, true, EntityCompareVersion)
				process = (*Store).ProcessEntityCompare
				table = "entity_alias_receipts"
				response = `{"same":false,"keep":null}`
			case EntityCandidatesStage:
				aliasEntityFixture(t, s, scope, "person", "Al", 1)
				aliasEntityFixture(t, s, scope, "person", "虚构齐鹤", 1)
				job = aliasNamesJob(t, s, scope)
				process = (*Store).ProcessEntityCandidates
				table = "entity_alias_candidates"
				response = `{"groups":[[1,2]]}`
			}
			fake.set(response)
			if _, err := s.pool.Exec(ctx, `CREATE FUNCTION fictional_write_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fictional business write failure'; END $$;
CREATE TRIGGER fictional_write_failure BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION fictional_write_failure()`); err != nil {
				t.Fatal(err)
			}
			if err := process(s, ctx, job); err == nil {
				t.Fatal("injected write failure did not fail")
			}
			var snapshots int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_model_results WHERE job_id=$1", job.ID).Scan(&snapshots); err != nil || snapshots != 1 {
				t.Fatal("paid response not durable", snapshots, err)
			}
			if _, err := s.pool.Exec(ctx, "DROP TRIGGER fictional_write_failure ON "+table); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, s.pool.Config().ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var originalDuration *int64
			if err := s.pool.QueryRow(ctx, "SELECT duration_ms FROM background_model_results WHERE job_id=$1", job.ID).Scan(&originalDuration); err != nil || originalDuration == nil {
				t.Fatal("paid duration not durable", err)
			}
			// Recovery must work with no available provider and no in-process cache.
			if err := process(reopened, ctx, job); err != nil {
				t.Fatal("recover paid write", err)
			}
			var state string
			var usage int
			if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE id=$1", job.ID).Scan(&state); err != nil || state != "done" {
				t.Fatal(state, err)
			}
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND job_id=$2", scope.OwnerID, job.ID).Scan(&usage); err != nil || usage != 1 {
				t.Fatal("usage duplicated or absent", usage, err)
			}
			var recoveredDuration *int64
			if err := s.pool.QueryRow(ctx, "SELECT duration_ms FROM model_usage WHERE owner_id=$1 AND job_id=$2", scope.OwnerID, job.ID).Scan(&recoveredDuration); err != nil || recoveredDuration == nil || *recoveredDuration != *originalDuration {
				t.Fatal("replay lost original duration", err)
			}
			if len(fake.all()) != 1 {
				t.Fatal("paid call repeated", len(fake.all()))
			}
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_model_results WHERE job_id=$1", job.ID).Scan(&snapshots); err != nil || snapshots != 0 {
				t.Fatal("committed response retained", snapshots, err)
			}
		})
	}
}

func TestPersistentBackgroundFailureKeepsIncreasingAttempts(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fictional unavailable", 503) })
	compareFixture(t, s, scope, "虚构需持续比较甲", "虚构需持续比较乙")
	job := compareJob(t, s, scope, false, CompareVersion)
	for _, attempt := range []int{1, 3, 8} {
		job.Attempts = attempt
		if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET state='leased',attempts=$2,lease_token=$3,lease_until=now()+interval '5 minutes' WHERE id=$1", job.ID, attempt, job.LeaseToken); err != nil {
			t.Fatal(err)
		}
		err := s.ProcessCompare(ctx, job)
		failure, ok := err.(*worker.JobError)
		if !ok || failure.Code != "model_call_failed" || failure.NoAttempt || failure.Until.IsZero() {
			t.Fatal("model failure did not count", err)
		}
		if err := s.Defer(ctx, job, failure.Code, failure.Until, failure.NoAttempt); err != nil {
			t.Fatal(err)
		}
		if !persistentBackgroundStage(job.Stage) {
			t.Fatal("background failure can exhaust")
		}
	}
	var failures, deferred int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE outcome='failure'),count(*) FILTER(WHERE outcome='deferred') FROM background_stage_events WHERE owner_id=$1 AND reason='model_call_failed'", scope.OwnerID).Scan(&failures, &deferred); err != nil || failures != 3 || deferred != 3 {
		t.Fatal("failed deferrals missing from health", failures, deferred, err)
	}

	var processed int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM claims WHERE owner_id=$1 AND compared<>0", scope.OwnerID).Scan(&processed); err != nil || processed != 0 {
		t.Fatal("failed compare marked processed", processed, err)
	}
	var inferred int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND input_estimated AND input_tokens>0", scope.OwnerID).Scan(&inferred); err != nil || inferred != 3 {
		t.Fatal("failed attempts not accounted", inferred, err)
	}
}

func TestNameScanKeepsUnaffectedBlocksAcrossCapacityBoundary(t *testing.T) {
	names := make([]entityCandidateName, 200)
	for i := range names {
		names[i] = entityCandidateName{Type: "person", Name: fmt.Sprintf("Fictitious person %03d", i), Ref: memory.Ref{Kind: memory.EntityKind, ID: memory.NewID(), Version: 1}, MemoryCount: 1, Positions: map[string]int64{"person": int64(i)}}
	}
	before := entityCandidateBatches(names, EntityCompareVersion)
	if len(before) != 3 {
		t.Fatal("self and cross blocks missing", len(before))
	}
	names = append(names, entityCandidateName{Type: "person", Name: "Fictitious person 200", Ref: memory.Ref{Kind: memory.EntityKind, ID: memory.NewID(), Version: 1}, MemoryCount: 1, Positions: map[string]int64{"person": 200}})
	after := entityCandidateBatches(names, EntityCompareVersion)
	markers := map[string]bool{}
	for _, b := range after {
		markers[b.Marker] = true
	}
	for _, b := range before {
		if !markers[b.Marker] {
			t.Fatal("capacity boundary invalidated an old block")
		}
	}
	for i := range names {
		names[i].MemoryCount += 10
		names[i].Ref.Version++
	}
	counted := entityCandidateBatches(names, EntityCompareVersion)
	for _, b := range counted {
		if !markers[b.Marker] {
			t.Fatal("memory counts or audit versions invalidated name receipt")
		}
	}
}

func TestLegacyNameScanReceiptTransfersWithoutAnotherModelCall(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"groups":[]}`)
	a, _ := aliasEntityFixture(t, s, scope, "person", "Fictitious Azure", 1)
	aliasEntityFixture(t, s, scope, "person", "虚构齐鹤", 1)
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		names, err := entityCandidateNamesTx(ctx, tx, scope.OwnerID, EntityCompareVersion)
		if err != nil {
			return err
		}
		old := entityCandidateBatchesFor(names, EntityCompareVersion, true)
		if len(old) != 1 {
			t.Fatal("legacy fixture", len(old))
		}
		if _, err := tx.Exec(ctx, "INSERT INTO background_markers(owner_id,record_id,record_version,stage) VALUES($1,$2,1,$3)", scope.OwnerID, a.ID, old[0].Marker); err != nil {
			return err
		}
		next, err := nextEntityCandidateBatchTx(ctx, tx, scope.OwnerID, EntityCompareVersion)
		if err == nil && next != nil {
			t.Fatal("paid legacy scan became pending", next)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
