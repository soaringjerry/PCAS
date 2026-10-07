package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase26G4T4OneMemoryEditRunsOnlyIts40AffectedComparisonCalls(t *testing.T) {
	f := phase26LoadFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	f.Context = ctx
	m := phase26NewModel(t, f)
	// Prior successful batch receipts are explicit preconditions, not measured
	// model calls. This test measures a single edit after an established library.
	phase26Exec(t, f, `UPDATE claims SET organized=$2 WHERE owner_id=$1`, f.Scope.OwnerID, OrganizeVersion)
	var prior []comparisonBatch
	if err := pgx.BeginFunc(ctx, f.Store.pool, func(tx pgx.Tx) error {
		if err := syncComparisonMembersTx(ctx, tx, f.Scope.OwnerID); err != nil {
			return err
		}
		var err error
		prior, err = comparisonPlanTx(ctx, tx, f.Scope.OwnerID, CompareVersion)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for _, b := range prior {
		rows = append(rows, map[string]any{"g": b.Group.Key, "a": b.A, "b": b.B, "f": b.Fingerprint})
	}
	raw, _ := json.Marshal(rows)
	phase26Exec(t, f, `INSERT INTO memory_comparison_batches(owner_id,group_key,block_a,block_b,fingerprint,rule,completed_at) SELECT $1,x.g,x.a,x.b,x.f,$3,now()-interval '2 hours' FROM jsonb_to_recordset($2::jsonb) x(g text,a bigint,b bigint,f text)`, f.Scope.OwnerID, raw, CompareVersion)
	if _, err := f.Store.ScheduleCompare(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Exec(t, f, `UPDATE claim_revisions SET value=to_jsonb('Fictitious single edited specimen'::text) WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, f.Claims[148])
	affected := map[string]bool{}
	for _, b := range prior {
		for _, ref := range b.Refs {
			if ref.ID == f.Claims[148] {
				affected[b.Group.Key+"/"+b.Fingerprint] = true
			}
		}
	}
	if len(affected) != 40 {
		t.Fatalf("fixture affected batches=%d expected40", len(affected))
	}
	m.mu.Lock()
	m.Reply = func(call phase26Call) string {
		if call.Stage == CompareStage {
			return `{"duplicates":[],"superseded":[]}`
		}
		return m.goldReply(call)
	}
	m.mu.Unlock()
	start := time.Now()
	for i := 0; i < 40; i++ {
		phase26Isolate(t, f, CompareStage)
		if _, err := f.Store.ScheduleCompare(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, CompareStage)
		j := phase26ClaimStage(t, f, CompareStage)
		if err := f.Store.ProcessCompare(ctx, j); err != nil {
			t.Fatal(err)
		}
		if (i+1)%10 == 0 {
			t.Logf("single edited memory actual_provider_calls=%d elapsed=%s", len(m.calls(CompareStage)), time.Since(start))
		}
	}
	if len(m.calls(CompareStage)) != 40 {
		t.Errorf("actual comparison calls=%d expected40", len(m.calls(CompareStage)))
	}
	for _, call := range m.calls(CompareStage) {
		var b comparisonBatch
		if err := json.Unmarshal([]byte(call.Prompt), &b); err != nil {
			t.Fatal(err)
		}
		has := false
		for _, r := range b.Refs {
			has = has || r.ID == memory.ID(f.Claims[148])
		}
		if !has {
			t.Errorf("unrelated batch reached provider %s %d/%d", b.Group.Key, b.A, b.B)
		}
	}
	for _, b := range prior {
		if affected[b.Group.Key+"/"+b.Fingerprint] {
			continue
		}
		var fingerprint string
		var completed time.Time
		if err := f.Store.pool.QueryRow(ctx, `SELECT fingerprint,completed_at FROM memory_comparison_batches WHERE owner_id=$1 AND group_key=$2 AND block_a=$3 AND block_b=$4`, f.Scope.OwnerID, b.Group.Key, b.A, b.B).Scan(&fingerprint, &completed); err != nil || fingerprint != b.Fingerprint || completed.After(start) {
			t.Errorf("unrelated prior receipt rewritten %s %d/%d %v", b.Group.Key, b.A, b.B, err)
		}
	}
	pending, err := f.Store.nextComparisonBatch(ctx, f.Scope.OwnerID, CompareVersion)
	if err != nil || pending != nil {
		t.Errorf("single edit leaves comparison pending %+v %v", pending, err)
	}
	t.Logf("single memory edit actual_model_calls=40 unchanged_batches=%d elapsed=%s; all calls include only affected blocks/groups", len(prior)-40, time.Since(start))
}
