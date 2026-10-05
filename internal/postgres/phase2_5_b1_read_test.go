package postgres_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B1_NeverOrganizedDefaults(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构人物陆青偏爱青色笔记本。")
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if string(object["category"]) != `"unknown"` {
		t.Errorf("category = %s", object["category"])
	}
	if _, exists := object["durable"]; exists {
		t.Error("never-organized memory exposes durable")
	}
	if string(object["groups"]) != `[]` {
		t.Errorf("groups = %s, want []", object["groups"])
	}
	var organized, attempts int
	if err := f.db.QueryRow(f.ctx, `SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID).Scan(&organized, &attempts); err != nil {
		t.Fatal(err)
	}
	if organized != 0 || attempts != 0 {
		t.Errorf("default counters = %d,%d", organized, attempts)
	}
	var page map[string]json.RawMessage
	f.get(t, "/v1/workspace/memories", &page)
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(page["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if string(items[0]["category"]) != `"unknown"` || string(items[0]["groups"]) != `[]` {
		t.Errorf("HTTP defaults = %v", items[0])
	}
	if _, exists := items[0]["durable"]; exists {
		t.Error("HTTP exposes unknown durable")
	}
}

func TestPhase25B1_StoredScalarLabelsSurviveLaggingState(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构季度汇报要求先展示结论。")
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "季度汇报")), Name: "季度汇报", Type: "topic"}
	f.labels(t, ref, "rule", false, 1, topic)
	for _, organized := range []int{1, 0} {
		t.Run(fmt.Sprintf("organized_%d", organized), func(t *testing.T) {
			f.exec(t, `UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID, organized)
			m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
			if err != nil {
				t.Fatal(err)
			}
			phase25B1AssertScalarLabels(t, m, "rule", false)
			var page workspace.MemoryPage
			f.get(t, "/v1/workspace/memories", &page)
			if len(page.Items) != 1 {
				t.Fatalf("items = %d", len(page.Items))
			}
			phase25B1AssertScalarLabels(t, page.Items[0], "rule", false)
			var state workspace.State
			f.get(t, "/v1/workspace", &state)
			if len(state.Memories) != 1 {
				t.Fatalf("snapshot items = %d", len(state.Memories))
			}
			phase25B1AssertScalarLabels(t, state.Memories[0], "rule", false)
		})
	}
}

func phase25B1AssertScalarLabels(t *testing.T, m workspace.Memory, category string, durable bool) {
	t.Helper()
	if m.Category != category || m.Durable == nil || *m.Durable != durable {
		t.Errorf("labels = %q,%v; want %q,%v", m.Category, m.Durable, category, durable)
	}
	if m.Kind != "fact" {
		t.Errorf("old kind changed to %q", m.Kind)
	}
}

func phase25B1AssertLabels(t *testing.T, m workspace.Memory, category string, durable bool, groups []workspace.MemoryGroup) {
	t.Helper()
	phase25B1AssertScalarLabels(t, m, category, durable)
	got := make(map[string]workspace.MemoryGroup)
	for _, g := range m.Groups {
		got[g.EntityID] = g
	}
	want := make(map[string]workspace.MemoryGroup)
	for _, g := range groups {
		want[g.EntityID] = g
	}
	if len(m.Groups) != len(groups) || !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %+v; want %+v", m.Groups, groups)
	}
}

func TestPhase25B1_X21_GroupAndCategoryHTTPPagination(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构月报")), Name: "虚构月报", Type: "topic"}
	other := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构周报")), Name: "虚构周报", Type: "topic"}
	want := make(map[string]bool)
	for i := 0; i < 5; i++ {
		ref := f.claim(t, fmt.Sprintf("虚构月报规则 %d：先写结论。", i))
		f.labels(t, ref, "rule", true, 1, topic)
		want[string(ref.ID)] = true
	}
	ref := f.claim(t, "虚构月报目标：编排插图。")
	f.labels(t, ref, "goal", true, 1, topic)
	ref = f.claim(t, "虚构周报规则：使用青色标题。")
	f.labels(t, ref, "rule", true, 1, other)
	base := "/v1/workspace/memories?group=" + url.QueryEscape(topic.EntityID) + "&category=rule&limit=2"
	seen := make(map[string]bool)
	cursor := ""
	for round := 0; round < 4; round++ {
		path := base
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var page workspace.MemoryPage
		f.get(t, path, &page)
		if page.Total != 5 {
			t.Errorf("total = %d, want 5", page.Total)
		}
		if len(page.Items) == 0 || len(page.Items) > 2 {
			t.Fatalf("page size = %d", len(page.Items))
		}
		for _, m := range page.Items {
			if !want[m.ID] || seen[m.ID] {
				t.Errorf("unexpected/duplicate memory %s", m.ID)
			}
			seen[m.ID] = true
			phase25B1AssertLabels(t, m, "rule", true, []workspace.MemoryGroup{topic})
		}
		if page.Next == "" {
			break
		}
		if page.Next == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.Next
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("pagination IDs = %v, want %v", seen, want)
	}
	var search workspace.MemoryPage
	f.get(t, base+"&q="+url.QueryEscape("规则 3"), &search)
	if search.Total != 1 || len(search.Items) != 1 {
		t.Errorf("conjunctive search = %+v", search)
	}
	var empty workspace.MemoryPage
	f.get(t, "/v1/workspace/memories?group="+url.QueryEscape(other.EntityID)+"&category=goal", &empty)
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Errorf("empty intersection = %+v", empty)
	}
	for _, check := range []struct {
		query string
		total int
	}{
		{"?group=" + url.QueryEscape(topic.EntityID), 6},
		{"?category=rule", 6},
	} {
		var page workspace.MemoryPage
		f.get(t, "/v1/workspace/memories"+check.query, &page)
		if page.Total != check.total || len(page.Items) != check.total {
			t.Errorf("single filter %s: total=%d items=%d, want %d", check.query, page.Total, len(page.Items), check.total)
		}
	}
}

func TestPhase25B1_R08_CorrectionInheritsStoredLabels(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构季度汇报要求先列三项结论。")
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "季度汇报")), Name: "季度汇报", Type: "topic"}
	f.labels(t, ref, "rule", false, 1, topic)
	person := f.entity(t, "person", "许澄")
	f.exec(t, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,'person')`, f.scope.OwnerID, ref.ID, ref.Version, person)
	var subject memory.ID
	if err := f.db.QueryRow(f.ctx, `SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal("虚构季度汇报要求先列两项结论。")
	if err != nil {
		t.Fatal(err)
	}
	replacement := memory.Claim{
		Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: subject,
		Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "confirmed",
	}
	corrected, err := f.store.Correct(f.ctx, f.scope, memory.CorrectRequest{Target: ref, Replacement: replacement, Reason: "虚构验收纠正"})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ID != ref.ID || corrected.Version != ref.Version+1 {
		t.Errorf("corrected ref = %+v", corrected)
	}
	var category string
	var durable *bool
	var organized, revisions int
	if err := f.db.QueryRow(f.ctx, `SELECT category,durable FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, corrected.ID, corrected.Version).Scan(&category, &durable); err != nil {
		t.Fatal(err)
	}
	if category != "rule" || durable == nil || *durable {
		t.Errorf("new version stored labels = %s,%v", category, durable)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT organized FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID).Scan(&organized); err != nil {
		t.Fatal(err)
	}
	if organized != 0 {
		t.Errorf("organized after correction = %d", organized)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 2 {
		t.Errorf("correction revisions = %d", revisions)
	}
	for _, version := range []int{ref.Version, corrected.Version} {
		var mentions int
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$3 AND entity_id=$4 AND role='topic'`, f.scope.OwnerID, ref.ID, version, topic.EntityID).Scan(&mentions); err != nil {
			t.Fatal(err)
		}
		if mentions != 1 {
			t.Errorf("version %d group mentions = %d", version, mentions)
		}
	}
}

func TestPhase25B1_FacetsRouteAvailable(t *testing.T) {
	// F-B1-6 was a contract path mistake, resolved by #187. Exercise the
	// corrected route independently of group behavior pending O1b (F-B1-4).
	f := phase25B1NewFixture(t)
	f.claim(t, "虚构用户许澄偏爱紫色积木。")
	var facets workspace.MemoryFacets
	f.get(t, "/v1/workspace/memory-facets", &facets)
}

func TestPhase25B1_FacetsCountsAndOwnerIsolation(t *testing.T) {
	f := phase25B1NewFixture(t)
	want := make(map[string]workspace.MemoryGroupFacet)
	for _, group := range []struct {
		typ, name string
		count     int
	}{
		{"project", "虚构灯塔计划", 1},
		{"topic", "虚构季度汇报", 2},
		{"topic", "虚构航模练习", 1},
		{"area", "工作", 3},
	} {
		id := f.entity(t, group.typ, group.name)
		for i := 0; i < group.count; i++ {
			ref := f.claim(t, fmt.Sprintf("虚构%s便签第%d条。", group.name, i))
			f.labels(t, ref, "progress", false, 1, workspace.MemoryGroup{EntityID: string(id), Name: group.name, Type: group.typ})
		}
		want[string(id)] = workspace.MemoryGroupFacet{EntityID: string(id), Name: group.name, Type: group.typ, Count: group.count}
	}
	f.entity(t, "topic", "虚构空分组")
	deletedGroup := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构已删分组")), Name: "虚构已删分组", Type: "topic"}
	deleted := f.claim(t, "虚构即将删除的便签。")
	f.labels(t, deleted, "event", false, 1, deletedGroup)
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{deleted}}); err != nil {
		t.Fatal(err)
	}
	originalScope := f.scope
	f.scope = memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictitious-other-owner", IsOwner: true}
	foreign := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构另一个用户分组")), Name: "虚构另一个用户分组", Type: "topic"}
	ref := f.claim(t, "虚构另一个用户的便签。")
	f.labels(t, ref, "rule", true, 1, foreign)
	f.scope = originalScope
	var facets workspace.MemoryFacets
	f.get(t, "/v1/workspace/memory-facets", &facets)
	got := make(map[string]workspace.MemoryGroupFacet)
	closedTypes := make(map[string]bool)
	lastType := ""
	lastCount := 0
	for _, group := range facets.Groups {
		if group.Type != lastType {
			if closedTypes[group.Type] {
				t.Errorf("type %s is not contiguous", group.Type)
			}
			if lastType != "" {
				closedTypes[lastType] = true
			}
			lastType, lastCount = group.Type, group.Count
		} else if group.Count > lastCount {
			t.Errorf("counts for %s are not descending", group.Type)
		}
		lastCount = group.Count
		if _, duplicate := got[group.EntityID]; duplicate {
			t.Errorf("duplicate facet %s", group.EntityID)
		}
		got[group.EntityID] = group
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("facets = %+v, want %+v", got, want)
	}
	storeFacets, err := f.store.MemoryFacets(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storeFacets.Groups, facets.Groups) {
		t.Errorf("store/HTTP groups differ")
	}
}

func TestPhase25B1_X15_DeleteOnlyMemoryLeavesGroupEntity(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构航模")), Name: "虚构航模", Type: "topic"}
	ref := f.claim(t, "虚构人物陆青本周拼装航模。")
	f.labels(t, ref, "event", false, 1, topic)
	untouched := f.claim(t, "虚构人物陆青偏爱青色笔记本。")
	before, err := f.store.GetMemory(f.ctx, f.scope, string(untouched.ID))
	if err != nil {
		t.Fatal(err)
	}
	var facets workspace.MemoryFacets
	f.get(t, "/v1/workspace/memory-facets", &facets)
	if len(facets.Groups) != 1 || facets.Groups[0].EntityID != topic.EntityID || facets.Groups[0].Count != 1 {
		t.Fatalf("before delete facets = %+v", facets.Groups)
	}
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"claims", "claim_revisions", "claim_mentions"} {
		column := "claim_id"
		if table == "claims" {
			column = "id"
		}
		var count int
		if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1 AND "+column+"=$2", f.scope.OwnerID, ref.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s retains %d deleted memory rows", table, count)
		}
	}
	var entities int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entities WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, topic.EntityID).Scan(&entities); err != nil {
		t.Fatal(err)
	}
	if entities != 1 {
		t.Errorf("remaining group entities = %d", entities)
	}
	f.get(t, "/v1/workspace/memory-facets", &facets)
	if len(facets.Groups) != 0 {
		t.Errorf("empty group still in facets: %+v", facets.Groups)
	}
	after, err := f.store.GetMemory(f.ctx, f.scope, string(untouched.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Exposure is time-dependent; persisted memory content must stay unchanged.
	before.Exposure, after.Exposure = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Errorf("unrelated memory changed: before=%+v after=%+v", before, after)
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 0, Total: 1, Version: 1}) {
		t.Errorf("after delete progress = %+v", state.Organize)
	}
}

func TestPhase25B1_StoredGroupsAndOrdinaryMentions(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构人物陆青在松湾的蓝鹭公司筹备季度汇报。")
	groups := []workspace.MemoryGroup{
		{EntityID: string(f.entity(t, "project", "季度汇报")), Name: "季度汇报", Type: "project"},
		{EntityID: string(f.entity(t, "topic", "排版练习")), Name: "排版练习", Type: "topic"},
		{EntityID: string(f.entity(t, "area", "工作")), Name: "工作", Type: "area"},
	}
	f.labels(t, ref, "progress", true, 0, groups...)
	ordinary := []workspace.MemoryMention{
		{EntityID: string(f.entity(t, "person", "陆青")), Name: "陆青", Role: "person"},
		{EntityID: string(f.entity(t, "place", "松湾")), Name: "松湾", Role: "place"},
		{EntityID: string(f.entity(t, "organization", "蓝鹭公司")), Name: "蓝鹭公司", Role: "organization"},
	}
	for _, mention := range ordinary {
		f.exec(t, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5)`, f.scope.OwnerID, ref.ID, ref.Version, mention.EntityID, mention.Role)
	}
	assert := func(m workspace.Memory) {
		phase25B1AssertLabels(t, m, "progress", true, groups)
		got := make(map[string]workspace.MemoryMention)
		for _, mention := range m.Mentions {
			got[mention.EntityID] = mention
		}
		want := make(map[string]workspace.MemoryMention)
		for _, mention := range ordinary {
			want[mention.EntityID] = mention
		}
		if len(m.Mentions) != len(ordinary) || !reflect.DeepEqual(got, want) {
			t.Errorf("ordinary mentions = %+v, want %+v", m.Mentions, ordinary)
		}
	}
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	assert(m)
	var page workspace.MemoryPage
	f.get(t, "/v1/workspace/memories", &page)
	if len(page.Items) != 1 {
		t.Fatalf("items = %d", len(page.Items))
	}
	assert(page.Items[0])
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if len(state.Memories) != 1 {
		t.Fatalf("snapshot memories = %d", len(state.Memories))
	}
	assert(state.Memories[0])
}

func TestPhase25B1_SnapshotProgressUsesStoredOrganized(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构整理已完成的便签。")
	f.labels(t, ref, "identity", true, 1)
	f.claim(t, "虚构首次整理前的便签。")
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 1, Total: 2, Version: 1}) {
		t.Errorf("progress = %+v", state.Organize)
	}
	f.exec(t, `UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID)
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 0, Total: 2, Version: 1}) {
		t.Errorf("lagging progress = %+v", state.Organize)
	}
}

func TestPhase25B1_R16_DeleteCascadesOnlyTargetGroupMentions(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构航模")), Name: "虚构航模", Type: "topic"}
	ref := f.claim(t, "虚构人物陆青拼装航模。")
	f.labels(t, ref, "event", false, 1, topic)
	untouched := f.claim(t, "虚构人物许澄偏爱紫色积木。")
	f.labels(t, untouched, "taste", true, 1, topic)
	var before, after string
	readUnrelated := func(target *string) {
		if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_object('claim',to_jsonb(c),'revision',to_jsonb(r),'mentions',(SELECT jsonb_agg(to_jsonb(m) ORDER BY entity_id,role) FROM claim_mentions m WHERE m.owner_id=c.owner_id AND m.claim_id=c.id))::text FROM claims c JOIN claim_revisions r ON r.owner_id=c.owner_id AND r.claim_id=c.id WHERE c.owner_id=$1 AND c.id=$2 AND r.version=$3`, f.scope.OwnerID, untouched.ID, untouched.Version).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	readUnrelated(&before)
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	readUnrelated(&after)
	if before != after {
		t.Error("deletion altered unrelated persisted claim, revision or mentions")
	}
	for _, table := range []string{"claims", "claim_revisions", "claim_mentions"} {
		column := "claim_id"
		if table == "claims" {
			column = "id"
		}
		var count int
		if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1 AND "+column+"=$2", f.scope.OwnerID, ref.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s retains %d target rows", table, count)
		}
	}
	var entityCount int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entities WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, topic.EntityID).Scan(&entityCount); err != nil {
		t.Fatal(err)
	}
	if entityCount != 1 {
		t.Errorf("shared group entity count = %d", entityCount)
	}
}
