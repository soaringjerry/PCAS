package postgres_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B2_TrustWithoutConfirmationGate(t *testing.T) {
	f := phase25B2NewFixture(t)
	for _, tc := range []struct{ name, text, acquisition, confirmation, trust string }{
		{"direct_unknown", "虚构陆青喜欢青色笔记本。", "direct", "unknown", "stated"},
		{"direct_candidate", "虚构陆青喜欢紫色文件夹。", "direct", "candidate", "stated"},
		{"direct_confirmed", "虚构陆青每周整理书桌。", "direct", "confirmed", "stated"},
		{"qualified", "虚构陆青可能考虑搬到松湾。", "direct", "unknown", "tentative"},
		{"reported", "虚构许澄说他明天参加航模课。", "reported", "unknown", "reported"},
		{"inferred", "推断虚构陆青喜欢绿色。", "inferred", "unknown", "inferred"},
		{"reported_qualified", "虚构许澄说他可能参加航模课。", "reported", "unknown", "reported"},
		{"inferred_qualified", "推断虚构陆青可能喜欢绿色。", "inferred", "unknown", "inferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := f.claimWith(t, tc.text, tc.acquisition, tc.confirmation, nil)
			m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
			if err != nil {
				t.Fatal(err)
			}
			if m.Trust != tc.trust || m.Confirmation != tc.confirmation || m.Acquisition != tc.acquisition {
				t.Errorf("trust/legacy fields=%q/%q/%q, want %q/%q/%q", m.Trust, m.Confirmation, m.Acquisition, tc.trust, tc.confirmation, tc.acquisition)
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(data, &obj); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"retired", "retiredBy", "mergedFrom"} {
				if _, ok := obj[key]; ok {
					t.Errorf("current unmerged memory exposes %s", key)
				}
			}
			f.assertRevisions(t, ref)
		})
	}
}

func TestPhase25B2_TrustCountsDistinctSources(t *testing.T) {
	f := phase25B2NewFixture(t)
	ref := f.claim(t, "虚构陆青喜欢青色笔记本。")
	var source memory.Ref
	if err := f.db.QueryRow(f.ctx, `SELECT source_id,source_version FROM evidence WHERE owner_id=$1 AND target_id=$2 LIMIT 1`, f.scope.OwnerID, ref.ID).Scan(&source.ID, &source.Version); err != nil {
		t.Fatal(err)
	}
	source.Kind = memory.SourceKind
	add := func(src memory.Ref) {
		t.Helper()
		// Commit creates graph records and rejects an evidence-only request.
		// This read test seeds additional evidence in the disposable database.
		f.exec(t, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,$6,'{}','direct','supports')`, f.scope.OwnerID, memory.NewID(), src.ID, src.Version, ref.ID, ref.Version)
	}
	add(source)
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.Trust != "stated" {
		t.Errorf("one source counted twice: %q", m.Trust)
	}
	other, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{Connector: "acceptance", ExternalID: string(memory.NewID()), Text: "另一份虚构便签：陆青喜欢青色笔记本。"})
	if err != nil {
		t.Fatal(err)
	}
	add(other.Ref)
	m, err = f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.Trust != "repeated" {
		t.Errorf("two independent sources: trust=%q, want repeated", m.Trust)
	}
	f.assertRevisions(t, ref)
}

func TestPhase25B2_CurrentHistoryAndMergedFromReads(t *testing.T) {
	f := phase25B2NewFixture(t)
	kept := f.claim(t, "虚构汇报的最终期限是周五。")
	duplicate := f.claim(t, "虚构汇报的期限是周五。")
	old := f.claim(t, "虚构汇报的期限是周三。")
	f.retire(t, duplicate, kept, "duplicate")
	f.retire(t, old, kept, "superseded")
	for _, path := range []string{"/v1/workspace/memories", "/v1/workspace/memories?retired=1"} {
		var page workspace.MemoryPage
		f.get(t, path, &page)
		if path == "/v1/workspace/memories" {
			phase25B234AssertIDs(t, page.Items, kept)
			if page.Total != 1 {
				t.Errorf("current total=%d", page.Total)
			}
			if len(page.Items) == 1 && page.Items[0].MergedFrom != 1 {
				t.Errorf("mergedFrom=%d, want 1", page.Items[0].MergedFrom)
			}
		} else {
			phase25B234AssertIDs(t, page.Items, duplicate, old)
			if page.Total != 2 {
				t.Errorf("historical total=%d", page.Total)
			}
			for _, m := range page.Items {
				if m.Retired == "" || m.RetiredBy != string(kept.ID) {
					t.Errorf("historical linkage=%+v", m)
				}
			}
		}
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	phase25B234AssertIDs(t, state.Memories, kept)
	f.assertRevisions(t, kept, duplicate, old)
}

func TestPhase25B2_CurrentOnlyFacetsAndPagination(t *testing.T) {
	f := phase25B2NewFixture(t)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构白鹭月报")), Name: "虚构白鹭月报", Type: "topic"}
	var current, history []memory.Ref
	for i := 0; i < 7; i++ {
		r := f.claim(t, fmt.Sprintf("虚构白鹭月报便签 %d。", i))
		f.labels(t, r, "progress", true, 1, g)
		if i%2 == 0 {
			current = append(current, r)
		} else {
			history = append(history, r)
		}
	}
	for _, r := range history {
		f.retire(t, r, current[0], "duplicate")
	}
	for _, historical := range []bool{false, true} {
		base := "/v1/workspace/memories?group=" + url.QueryEscape(g.EntityID) + "&limit=2"
		want := current
		if historical {
			base += "&retired=1"
			want = history
		}
		var all []workspace.Memory
		cursor := ""
		seen := map[string]bool{}
		for n := 0; n < 6; n++ {
			path := base
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			var p workspace.MemoryPage
			f.get(t, path, &p)
			if p.Total != len(want) {
				t.Errorf("historical=%v total=%d want=%d", historical, p.Total, len(want))
			}
			all = append(all, p.Items...)
			if p.Next == "" {
				break
			}
			if seen[p.Next] {
				t.Fatal("pagination cursor cycle")
			}
			seen[p.Next] = true
			cursor = p.Next
		}
		phase25B234AssertIDs(t, all, want...)
	}
	var facets workspace.MemoryFacets
	f.get(t, "/v1/workspace/memory-facets", &facets)
	if len(facets.Groups) != 1 || facets.Groups[0].Count != len(current) {
		t.Errorf("facets include retired memories: %+v", facets.Groups)
	}
}

func TestPhase25B2_X2_9_DeleteSurvivorKeepsHistory(t *testing.T) {
	f := phase25B2NewFixture(t)
	kept := f.claim(t, "虚构汇报在周五交。")
	a := f.claim(t, "虚构汇报周五交。")
	b := f.claim(t, "虚构汇报周五提交。")
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构删除保留项")), Type: "project"}
	for _, r := range []memory.Ref{kept, a, b} {
		f.labels(t, r, "progress", true, 1, g)
	}
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		keep := phase25B2N(in, "虚构汇报在周五交。")
		out.Duplicates = []phase25B2WireDuplicate{{Keep: keep, Members: []int{keep, phase25B2N(in, "虚构汇报周五交。"), phase25B2N(in, "虚构汇报周五提交。")}}}
		return out
	})
	f.runCompare(t)
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{kept}}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND retired='duplicate' AND retired_by=$3`, f.scope.OwnerID, []string{string(a.ID), string(b.ID)}, kept.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("historical children=%d, want 2 (coordinator chose preservation)", n)
	}
	var p workspace.MemoryPage
	f.get(t, "/v1/workspace/memories", &p)
	phase25B234AssertIDs(t, p.Items)
	f.get(t, "/v1/workspace/memories?retired=1", &p)
	phase25B234AssertIDs(t, p.Items, a, b)
	f.assertRevisions(t, a, b)
}

// This initial random sequence tests consumers of frozen retirement state.
// End-to-end comparison/restore operations will be added through the adapter,
// once the coordinator supplies entrypoints and the model wire format.
func TestPhase25B2_RandomReadSequence(t *testing.T) {
	const seed int64 = 252502
	f := phase25B2NewFixture(t)
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed=%d", seed)
	keeper := f.claim(t, "虚构固定的保留记忆。")
	current := []memory.Ref{keeper}
	var retired []memory.Ref
	for step := 0; step < 40; step++ {
		switch op := rng.Intn(4); op {
		case 0:
			current = append(current, f.claim(t, fmt.Sprintf("虚构随机便签 %d。", step)))
		case 1:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				r := current[i]
				f.retire(t, r, keeper, []string{"duplicate", "superseded"}[rng.Intn(2)])
				retired = append(retired, r)
				current = append(current[:i], current[i+1:]...)
			}
		case 2:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				r := current[i]
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{r}}); err != nil {
					t.Fatal(err)
				}
				current = append(current[:i], current[i+1:]...)
			}
		case 3:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				current[i] = f.correct(t, current[i], fmt.Sprintf("虚构随机纠正 %d。", step))
			}
		}
		p, err := f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		phase25B234AssertIDs(t, p.Items, current...)
		h, err := f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Retired: true, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		phase25B234AssertIDs(t, h.Items, retired...)
		for _, m := range p.Items {
			if m.Retired != "" || m.RetiredBy != "" {
				t.Errorf("step %d: retired linkage leaked into current view", step)
			}
		}
		f.assertRevisions(t, append(append([]memory.Ref{}, current...), retired...)...)
		if t.Failed() {
			t.Fatalf("random sequence failed at seed=%d step=%d", seed, step)
		}
	}
}
