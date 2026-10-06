package postgres_test

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"

	"strings"

	"testing"
	"time"

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

// A positive baseline prevents an empty skeleton from satisfying safety
// invariants vacuously. Three fixed current items keep the group above cutoff.

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

func TestPhase25B3_ReadOwnerIsolation(t *testing.T) {
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
