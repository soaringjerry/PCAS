package postgres_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Seed only derived data for read/delete tests. Building tests must use the
// backend adapter, not this helper. Inputs remain ordinary public Commit data.
func (f *phase25B234Fixture) card(t *testing.T, group workspace.MemoryGroup, refs []memory.Ref, stale bool) string {
	t.Helper()
	key := "entity:" + group.EntityID
	f.exec(t, `INSERT INTO status_cards(owner_id,key,kind,entity_id,name,rule,built_at,stale) VALUES($1,$2,$3,$4,$5,1,now(),$6) ON CONFLICT(owner_id,key) DO UPDATE SET built_at=excluded.built_at,stale=excluded.stale`, f.scope.OwnerID, key, group.Type, group.EntityID, group.Name, stale)
	f.exec(t, `DELETE FROM status_card_items WHERE owner_id=$1 AND key=$2`, f.scope.OwnerID, key)
	for i, r := range refs {
		f.exec(t, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version) VALUES($1,$2,$3,$4,$5,$6)`, f.scope.OwnerID, key, []string{"status", "next", "decided"}[i%3], i, r.ID, r.Version)
	}
	return key
}

func (f *phase25B234Fixture) cardGroup(t *testing.T, n int) (workspace.MemoryGroup, []memory.Ref) {
	t.Helper()
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构白鹭月报")), Name: "虚构白鹭月报", Type: "project"}
	var refs []memory.Ref
	for i := 0; i < n; i++ {
		r := f.claim(t, fmt.Sprintf("虚构白鹭月报原文 %d：先写结论，再放图表。", i))
		f.labels(t, r, "progress", true, 1, g)
		refs = append(refs, r)
	}
	return g, refs
}

func (f *phase25B234Fixture) handover(t *testing.T, key string, stale bool) {
	t.Helper()
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	var sections []string
	for i, title := range []string{"他是谁和现在的处境", "怎么跟他配合", "现在手上的事", "时间和节奏", "资源和限制", "口味和标准", "重要的人", "他的叫法", "他看重什么"} {
		text := "（暂无依据）"
		if i == 2 {
			text = "虚构陆青正在准备白鹭月报。"
		}
		sections = append(sections, title+"\n"+text)
	}
	// The contract does not freeze the object keys inside depends. Use its
	// valid empty default for these read/delete tests, never guess that format.
	f.exec(t, `INSERT INTO handovers(owner_id,body,rule,built_at,stale,depends) SELECT $1,$4,1,now(),$3,'[]'::jsonb FROM status_cards WHERE owner_id=$1 AND key=$2 ON CONFLICT(owner_id) DO UPDATE SET body=excluded.body,built_at=excluded.built_at,stale=excluded.stale,depends=excluded.depends`, f.scope.OwnerID, key, stale, strings.Join(sections, "\n\n"))
}

func (f *phase25B234Fixture) deadline(t *testing.T, r memory.Ref, at time.Time) string {
	t.Helper()
	id := string(memory.NewID())
	f.exec(t, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,title) VALUES($1,$2,$3,$4,'deadline',$5,'虚构白鹭月报期限')`, f.scope.OwnerID, id, r.ID, r.Version, at)
	return id
}

func (f *phase25B234Fixture) readCards(t *testing.T, keys ...string) []workspace.StatusCard {
	t.Helper()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	cards, err := f.store.StatusCardsTx(f.ctx, tx, f.scope, keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	return cards
}

func phase25B3Items(cards []workspace.StatusCard) []workspace.Memory {
	var items []workspace.Memory
	for _, c := range cards {
		for _, field := range c.Fields {
			items = append(items, field.Items...)
		}
	}
	return items
}

func TestPhase25B3_EmptyAboutFallback(t *testing.T) {
	f := phase25B234NewFixture(t)
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if a.Handover.Body != "" || len(a.Cards) != 0 || len(a.Deadlines) != 0 || a.Building.Done != 0 || a.Building.Total != 0 {
		t.Errorf("empty layer=%+v", a)
	}
	var raw map[string]json.RawMessage
	f.get(t, "/v1/workspace/about", &raw)
	for _, key := range []string{"cards", "deadlines"} {
		if string(raw[key]) != "[]" {
			t.Errorf("empty %s=%s, want []", key, raw[key])
		}
	}
}

func TestPhase25B3_OriginalCardItemsDirectoryAndFullReads(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 12)
	key := f.card(t, g, refs, false)
	var index workspace.About
	f.get(t, "/v1/workspace/about", &index)
	if len(index.Cards) != 1 {
		t.Fatalf("directory cards=%d, want 1", len(index.Cards))
	}
	if index.Cards[0].Fields == nil || len(index.Cards[0].Fields) != 0 {
		t.Errorf("directory fields=%+v, want []", index.Cards[0].Fields)
	}
	var full workspace.About
	f.get(t, "/v1/workspace/about?key="+url.QueryEscape(key), &full)
	phase25B234AssertIDs(t, phase25B3Items(full.Cards), refs...)
	cards := f.readCards(t, key)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs...)
	for _, m := range phase25B3Items(cards) {
		original, err := f.store.GetMemory(f.ctx, f.scope, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		m.Exposure = 0
		original.Exposure = 0
		if !reflect.DeepEqual(m, original) {
			t.Errorf("card item is not complete original memory: %+v / %+v", m, original)
		}
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_6_CorrectedItemOmittedAndStale(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 5)
	key := f.card(t, g, refs, false)
	newRef := f.correct(t, refs[0], "虚构纠正：白鹭月报先放图表。")
	cards := f.readCards(t, key)
	if len(cards) != 1 {
		t.Fatalf("stale card missing: %+v", cards)
	}
	phase25B234AssertIDs(t, phase25B3Items(cards), refs[1:]...)
	if !cards[0].Stale {
		t.Error("card not marked stale after invalid revision")
	}
	var stale bool
	if err := f.db.QueryRow(f.ctx, `SELECT stale FROM status_cards WHERE owner_id=$1 AND key=$2`, f.scope.OwnerID, key).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("stale state not persisted")
	}
	f.assertRevisions(t, append([]memory.Ref{newRef}, refs[1:]...)...)
}

func TestPhase25B3_X3_7_SupersededItemOmittedAndStale(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 5)
	key := f.card(t, g, refs, false)
	f.retire(t, refs[0], refs[1], "superseded")
	cards := f.readCards(t, key)
	if len(cards) != 1 {
		t.Fatalf("remaining stale card missing: %+v", cards)
	}
	phase25B234AssertIDs(t, phase25B3Items(cards), refs[1:]...)
	if !cards[0].Stale {
		t.Error("retired item did not stale card")
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_8_DeleteCascadesAndStalesHandover(t *testing.T) {
	t.Skip("finding F-B3-4")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 4)
	key := f.card(t, g, refs, false)
	f.handover(t, key, false)
	f.deadline(t, refs[0], time.Now().Add(48*time.Hour))
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[0]}}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"status_card_items", "deadlines"} {
		var n int
		if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1 AND claim_id=$2", f.scope.OwnerID, refs[0].ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s retains deleted item", table)
		}
	}
	var stale bool
	if err := f.db.QueryRow(f.ctx, `SELECT stale FROM handovers WHERE owner_id=$1`, f.scope.OwnerID).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("deletion did not mark handover stale")
	}
	cards := f.readCards(t, key)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs[1:]...)
	f.assertRevisions(t, refs[1:]...)
}

func TestPhase25B3_X3_13_TwoCurrentMemoriesHideCard(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	key := f.card(t, g, refs, false)
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), refs...)
	if t.Failed() {
		t.Fatal("positive card baseline failed")
	}
	f.retire(t, refs[0], refs[1], "duplicate")
	if cards := f.readCards(t, key); len(cards) != 0 {
		t.Errorf("under-threshold card returned: %+v", cards)
	}
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if len(a.Cards) != 0 {
		t.Errorf("under-threshold directory returned: %+v", a.Cards)
	}
}

func TestPhase25B3_DeleteAllMemoriesHidesCard(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	key := f.card(t, g, refs, false)
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), refs...)
	if t.Failed() {
		t.Fatal("positive card baseline failed")
	}
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: refs}); err != nil {
		t.Fatal(err)
	}
	if cards := f.readCards(t, key); len(cards) != 0 {
		t.Errorf("empty group card returned: %+v", cards)
	}
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if len(a.Cards) != 0 {
		t.Errorf("empty group remains in directory: %+v", a.Cards)
	}
}

func TestPhase25B3_StaleCardAndHandoverRemainReadable(t *testing.T) {
	t.Skip("finding F-B3-2")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	key := f.card(t, g, refs, true)
	f.handover(t, key, true)
	var a workspace.About
	f.get(t, "/v1/workspace/about?key="+url.QueryEscape(key), &a)
	phase25B234AssertIDs(t, phase25B3Items(a.Cards), refs...)
	if a.Handover.Body == "" || !a.Handover.Stale {
		t.Errorf("stale handover unavailable: %+v", a.Handover)
	}
	if len(a.Cards) == 1 && !a.Cards[0].Stale {
		t.Error("stored stale flag missing")
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_DeadlinesCurrentFutureOrderedAndLimited(t *testing.T) {
	t.Skip("finding F-B3-3")
	f := phase25B234NewFixture(t)
	_, refs := f.cardGroup(t, 5)
	now := time.Now().UTC().Truncate(time.Second)
	later := f.deadline(t, refs[0], now.Add(72*time.Hour))
	earlier := f.deadline(t, refs[1], now.Add(24*time.Hour))
	f.deadline(t, refs[2], now.Add(-24*time.Hour))
	f.deadline(t, refs[3], now.Add(12*time.Hour))
	f.retire(t, refs[3], refs[0], "superseded")
	f.deadline(t, refs[4], now.Add(6*time.Hour))
	f.correct(t, refs[4], "虚构纠正：这次没有期限。")
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	d, err := f.store.DeadlinesTx(f.ctx, tx, f.scope, now, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 {
		t.Errorf("valid deadlines=%d, want 2: %+v", len(d), d)
	} else if d[0].ID != earlier || d[1].ID != later {
		t.Errorf("deadline order=%+v", d)
	}
	d, err = f.store.DeadlinesTx(f.ctx, tx, f.scope, now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0].ID != earlier {
		t.Errorf("limited deadline result=%+v", d)
	}
}

func TestPhase25B3_ReadOwnerIsolation(t *testing.T) {
	t.Skip("finding F-B3-1")
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	key := f.card(t, g, refs, false)
	f.handover(t, key, false)
	f.deadline(t, refs[0], time.Now().Add(48*time.Hour))
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), refs...)
	if t.Failed() {
		t.Fatal("positive card baseline failed")
	}
	foreign := memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictitious-other-owner", IsOwner: true}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	checks := []func(pgx.Tx) error{
		func(tx pgx.Tx) error {
			v, e := f.store.StatusCardsTx(f.ctx, tx, foreign, []string{key})
			if len(v) > 0 {
				t.Error("foreign card leaked")
			}
			return e
		},
		func(tx pgx.Tx) error {
			v, e := f.store.StatusCardIndexTx(f.ctx, tx, foreign)
			if len(v) > 0 {
				t.Error("foreign directory leaked")
			}
			return e
		},
		func(tx pgx.Tx) error {
			v, e := f.store.DeadlinesTx(f.ctx, tx, foreign, time.Now(), 15)
			if len(v) > 0 {
				t.Error("foreign deadline leaked")
			}
			return e
		},
		func(tx pgx.Tx) error {
			v, e := f.store.HandoverTx(f.ctx, tx, foreign)
			if v.Body != "" {
				t.Error("foreign handover leaked")
			}
			return e
		},
	}
	for _, check := range checks {
		if err := check(tx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPhase25B3_RandomCardReadSequence(t *testing.T) {
	t.Skip("finding F-B3-1")
	const seed int64 = 252503
	f := phase25B234NewFixture(t)
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed=%d", seed)
	g, refs := f.cardGroup(t, 8)
	key := f.card(t, g, refs, false)
	current := append([]memory.Ref{}, refs...)
	// A positive baseline prevents an empty skeleton from satisfying safety
	// invariants vacuously. Three fixed current items keep the group above cutoff.
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), current...)
	if t.Failed() {
		t.Fatal("positive baseline failed")
	}
	for step := 0; step < 36; step++ {
		switch rng.Intn(4) {
		case 0:
			if len(current) > 3 {
				i := 3 + rng.Intn(len(current)-3)
				current[i] = f.correct(t, current[i], fmt.Sprintf("虚构随机卡片纠正 %d。", step))
			}
		case 1:
			if len(current) > 3 {
				i := 3 + rng.Intn(len(current)-3)
				f.retire(t, current[i], current[0], "superseded")
				current = append(current[:i], current[i+1:]...)
			}
		case 2:
			if len(current) > 3 {
				i := 3 + rng.Intn(len(current)-3)
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{current[i]}}); err != nil {
					t.Fatal(err)
				}
				current = append(current[:i], current[i+1:]...)
			}
		case 3:
			key = f.card(t, g, current, false)
		}
		want := map[string]int{}
		for _, r := range current {
			want[string(r.ID)] = r.Version
		}
		seen := map[string]bool{}
		cards := f.readCards(t, key)
		if len(cards) != 1 {
			t.Fatalf("seed=%d step=%d missing surviving card", seed, step)
		}
		for _, m := range phase25B3Items(cards) {
			if want[m.ID] != m.Version || seen[m.ID] || m.Retired != "" {
				t.Errorf("seed=%d step=%d invalid/repeated item %+v", seed, step, m)
			}
			seen[m.ID] = true
		}
		f.assertRevisions(t, current...)
		if t.Failed() {
			t.Fatalf("seed=%d step=%d", seed, step)
		}
	}
}
