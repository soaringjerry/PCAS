package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPhase26A7A8NegativeNamesAndScanReceiptsIgnoreMemoryCounts(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	s, ctx := f.Store, f.Context
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	// No shared character or letter. Only the fictitious model may propose them.
	phase26Exec(t, f, `UPDATE entity_versions SET name='QZ' WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, f.Entities[0])
	phase26Exec(t, f, `UPDATE entity_versions SET name='北岸' WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, f.Entities[4])
	m.mu.Lock()
	m.Reply = func(call phase26Call) string {
		if call.Stage != EntityCandidatesStage {
			return m.goldReply(call)
		}
		var p struct {
			Entities []struct {
				N    int
				Name string
			}
		}
		_ = json.Unmarshal([]byte(call.Prompt), &p)
		a, b := 0, 0
		for _, e := range p.Entities {
			if e.Name == "QZ" || e.Name == "QZPrime" {
				a = e.N
			}
			if e.Name == "北岸" {
				b = e.N
			}
		}
		if a != 0 && b != 0 {
			return fmt.Sprintf(`{"groups":[[%d,%d]]}`, a, b)
		}
		return `{"groups":[]}`
	}
	m.mu.Unlock()
	phase26Isolate(t, f, EntityCandidatesStage)
	if _, err := phase26Schedule(ctx, s, EntityCandidatesStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, EntityCandidatesStage)
	scan := phase26ClaimStage(t, f, EntityCandidatesStage)
	if err := phase26Process(ctx, s, scan); err != nil {
		t.Fatal(err)
	}
	var proposed int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM entity_alias_candidates WHERE owner_id=$1 AND left_id=least($2::uuid,$3::uuid) AND right_id=greatest($2::uuid,$3::uuid)`, f.Scope.OwnerID, f.Entities[0], f.Entities[4]).Scan(&proposed); err != nil || proposed != 1 {
		t.Fatalf("model-only candidate count=%d err=%v", proposed, err)
	}
	phase26Isolate(t, f, EntityCompareStage)
	if _, err := phase26Schedule(ctx, s, EntityCompareStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, EntityCompareStage)
	j := phase26ClaimStage(t, f, EntityCompareStage)
	// A held legacy-card table lock must have no effect on a negative decision.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE status_cards,status_card_items IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := phase26Process(ctx, s, j); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	before := phase26Digest(t, f, []string{"entity_alias_receipts", "background_markers"})
	phase26Exec(t, f, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'person') ON CONFLICT DO NOTHING`, f.Scope.OwnerID, f.Claims[4999], f.Entities[0])
	if _, err := s.ScheduleCompare(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.entity_compare:%' AND state IN('queued','leased')`, f.Scope.OwnerID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("memory count invalidated negative decision pending=%d err=%v", pending, err)
	}
	after := phase26Digest(t, f, []string{"entity_alias_receipts", "background_markers"})
	for table, hash := range before {
		if after[table] != hash {
			t.Errorf("count change rewrote %s", table)
		}
	}
	var batch *entityCandidateBatch
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		batch, err = nextEntityCandidateBatchTx(ctx, tx, f.Scope.OwnerID, EntityCompareVersion)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var completed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM background_markers WHERE owner_id=$1 AND stage=$2)`, f.Scope.OwnerID, batch.Marker).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Error("completed scan batch repeated after memory-count change")
	}
	phase26Exec(t, f, `UPDATE entity_versions SET name='QZPrime' WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, f.Entities[0])
	phase26Isolate(t, f, EntityCandidatesStage)
	if _, err := phase26Schedule(ctx, s, EntityCandidatesStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, EntityCandidatesStage)
	scan = phase26ClaimStage(t, f, EntityCandidatesStage)
	if err := phase26Process(ctx, s, scan); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, EntityCompareStage)
	if _, err := phase26Schedule(ctx, s, EntityCompareStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, EntityCompareStage)
	j = phase26ClaimStage(t, f, EntityCompareStage)
	if err := phase26Process(ctx, s, j); err != nil {
		t.Fatal(err)
	}
	if len(m.calls(EntityCompareStage)) != 2 || len(m.calls(EntityCandidatesStage)) != 2 {
		t.Errorf("rename recheck provider counts confirm=%d scan=%d", len(m.calls(EntityCompareStage)), len(m.calls(EntityCandidatesStage)))
	}
	t.Logf("negative decision ignores held card locks; elapsed=%s; count change causes zero recheck; rename causes one scan + one confirmation", time.Since(start))
}
