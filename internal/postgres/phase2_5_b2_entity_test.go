package postgres_test

import (
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"testing"
)

type phase25B2InputEntity struct {
	N        int                    `json:"n"`
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Memories []phase25B2InputMemory `json:"memories"`
}

func phase25B2Entities(r phase25B234ModelRequest) ([]phase25B2InputEntity, error) {
	text, e := phase25B3Prompt(r)
	if e != nil {
		return nil, e
	}
	var in struct {
		Entities []phase25B2InputEntity `json:"entities"`
	}
	e = json.Unmarshal([]byte(text), &in)
	return in.Entities, e
}
func (f *phase25B234Fixture) personTexts(t *testing.T, g workspace.MemoryGroup, id memory.ID, texts ...string) []memory.Ref {
	t.Helper()
	var refs []memory.Ref
	for _, text := range texts {
		r := f.claim(t, text)
		f.exec(t, `UPDATE claim_revisions SET subject_id=$3 WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, r.ID, id)
		f.labels(t, r, "other_person", true, 1, g, workspace.MemoryGroup{EntityID: string(id), Type: "person"})
		refs = append(refs, r)
	}
	return refs
}
func phase25B2EntityScenario(t *testing.T, undo bool) {
	t.Helper()
	f := phase25B2NewFixture(t)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "白鹭月报")), Type: "project", Name: "白鹭月报"}
	a, b := f.entity(t, "person", "小陈"), f.entity(t, "person", "陈亮")
	ra := f.personTexts(t, g, a, "虚构小陈负责白鹭月报图表。")
	rb := f.personTexts(t, g, b, "虚构陈亮负责白鹭月报提交。")
	calls := 0
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		entities, e := phase25B2Entities(r)
		if e != nil {
			t.Error(e)
			return phase25B234ModelReply{status: 400}
		}
		if len(entities) == 0 {
			return phase25B2JSON(phase25B2Empty())
		}
		calls++
		if len(entities) != 2 {
			t.Errorf("entity pair size=%d", len(entities))
		}
		keep := 0
		for _, ent := range entities {
			if len(ent.Memories) > 10 {
				t.Error("entity input exceeds 10 memories")
			}
			if ent.Name == "陈亮" {
				keep = ent.N
			}
		}
		if keep == 0 {
			t.Errorf("candidate contains no 陈亮: %+v", entities)
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		data, _ := json.Marshal(map[string]any{"same": true, "keep": keep})
		return phase25B234ModelReply{content: string(data)}
	})
	f.runCompare(t)
	if calls != 1 {
		t.Errorf("entity merge calls=%d want 1", calls)
	}
	var kept string
	if err := f.db.QueryRow(f.ctx, `SELECT kept_id FROM entity_merges WHERE owner_id=$1 AND merged_id=$2 AND undone_at IS NULL`, f.scope.OwnerID, a).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != string(b) {
		t.Errorf("kept=%s want %s", kept, b)
	}
	for _, r := range append(ra, rb...) {
		var subject string
		if err := f.db.QueryRow(f.ctx, `SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, r.ID, r.Version).Scan(&subject); err != nil {
			t.Fatal(err)
		}
		if subject != string(b) {
			t.Errorf("subject=%s", subject)
		}
	}
	for _, name := range []string{"小陈", "陈亮"} {
		phase25B4AssertRecallIDs(t, f.recall(t, "关于"+name+"的白鹭月报信息"), append(ra, rb...)...)
	}
	// Kept separate so a public action-format handoff does not hide merge coverage.
	if undo {
		f.compareAction(t, []string{"undoEntityMerge"}, string(a))
		for _, r := range ra {
			var subject string
			if err := f.db.QueryRow(f.ctx, `SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, r.ID, r.Version).Scan(&subject); err != nil {
				t.Fatal(err)
			}
			if subject != string(a) {
				t.Errorf("undo old subject=%s want=%s", subject, a)
			}
		}
		for _, r := range rb {
			var subject string
			if err := f.db.QueryRow(f.ctx, `SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, r.ID, r.Version).Scan(&subject); err != nil {
				t.Fatal(err)
			}
			if subject != string(b) {
				t.Errorf("undo kept subject=%s", subject)
			}
		}
		for _, pair := range []struct {
			id   memory.ID
			refs []memory.Ref
		}{{a, ra}, {b, rb}} {
			p, err := f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Entity: string(pair.id), Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			phase25B234AssertIDs(t, p.Items, pair.refs...)
		}
		var undone bool
		if err := f.db.QueryRow(f.ctx, `SELECT undone_at IS NOT NULL FROM entity_merges WHERE owner_id=$1 AND merged_id=$2`, f.scope.OwnerID, a).Scan(&undone); err != nil {
			t.Fatal(err)
		}
		if !undone {
			t.Error("merge history not marked undone")
		}
	}
	f.assertRevisions(t, append(ra, rb...)...)
}
func TestPhase25B2_X2_10_EntityMergeAliasesAndSubjects(t *testing.T) {
	phase25B2EntityScenario(t, false)
}
func TestPhase25B2_X2_10_EntityMergeUndo(t *testing.T) { phase25B2EntityScenario(t, true) }

func TestPhase25B2_X2_11_NegativeAndVagueCandidatesDoNotMerge(t *testing.T) {
	f := phase25B2NewFixture(t)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构棋社")), Type: "topic"}
	names := []string{"老王", "老王", "那个老王", "这位王师傅", "许澄"}
	var refs []memory.Ref
	for i, name := range names {
		id := f.entity(t, "person", name)
		refs = append(refs, f.personTexts(t, g, id, []string{"虚构棋社老王在松湾。", "虚构棋社另一位老王在竹洲。", "虚构模糊称呼不得合并。", "虚构模糊指代不得合并。", "虚构许澄负责棋社报名。"}[i])...)
	}
	called := 0
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		entities, e := phase25B2Entities(r)
		if e != nil {
			t.Error(e)
			return phase25B234ModelReply{status: 400}
		}
		if len(entities) == 0 {
			return phase25B2JSON(phase25B2Empty())
		}
		called++
		for _, ent := range entities {
			if ent.Name == "那个老王" || ent.Name == "这位王师傅" {
				t.Error("vague name became candidate")
			}
		}
		return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
	})
	// Only a single candidate batch is required; a negative pair may remain
	// eligible in this version. Never burn the hourly allowance by busy polling.
	f.scheduleCompare(t)
	for i := 0; i < 8; i++ {
		stage := f.compareJob(t)
		if stage == "" {
			break
		}
	}
	if called == 0 {
		t.Error("same-name pair was not considered")
	}
	var merges int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_merges WHERE owner_id=$1 AND undone_at IS NULL`, f.scope.OwnerID).Scan(&merges); err != nil {
		t.Fatal(err)
	}
	if merges != 0 {
		t.Errorf("negative/vague entities merged=%d", merges)
	}
	f.assertRevisions(t, refs...)
}
