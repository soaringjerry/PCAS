package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26B1C8LegacyCardsFrozenUnreadAndFallbackAnswers(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	phase26Exec(t, f, `INSERT INTO status_cards(owner_id,key,kind,name,rule,stale) VALUES($1,'self:identity','self','Fictitious frozen legacy card',1,true)`, f.Scope.OwnerID)
	phase26Exec(t, f, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version,applies_to) VALUES($1,'self:identity','status',1,$2,1,'')`, f.Scope.OwnerID, f.Claims[366])
	before := phase26Digest(t, f, []string{"status_cards", "status_card_items"})
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCandidatesStage, HandoverStage} {
		if _, err := phase26Schedule(f.Context, f.Store, stage); err != nil {
			t.Fatal(err)
		}
	}
	var builds int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.card:%' AND state<>'done'`, f.Scope.OwnerID).Scan(&builds); err != nil || builds != 0 {
		t.Fatalf("card builds still queued=%d err=%v", builds, err)
	}
	tx, err := f.Store.pool.Begin(f.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(f.Context, `LOCK TABLE status_cards,status_card_items IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		out workspace.DeskTurnResponse
		err error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(f.Context, 10*time.Second)
	defer cancel()
	go func() {
		out, err := f.Store.DeskTurn(WithMemoryTier(ctx, "light"), f.Scope, workspace.DeskTurnRequest{AgentID: "phase26", RequestID: string(memory.NewID()), Text: "Please answer this fictitious question without a handover."})
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.out.Turn.Reply != "Fictitious acceptance reply." {
			t.Fatalf("no-handover fallback failed %+v %v", r.out.Turn, r.err)
		}
	case <-ctx.Done():
		_ = tx.Rollback(f.Context)
		r := <-done
		t.Fatalf("foreground read blocked on legacy cards: %v", r.err)
	}
	if err := tx.Rollback(f.Context); err != nil {
		t.Fatal(err)
	}
	after := phase26Digest(t, f, []string{"status_cards", "status_card_items"})
	for table, hash := range before {
		if after[table] != hash {
			t.Errorf("frozen legacy table changed: %s", table)
		}
	}
	if len(m.calls("secretary")) != 1 {
		t.Error("fallback made an unexpected extra primary call")
	}
	t.Log("legacy rows survive unchanged; no card jobs; legacy table locks do not block no-handover secretary fallback")
}
