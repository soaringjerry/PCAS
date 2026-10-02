package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type b2LibraryFixture struct {
	Refs           []memory.Ref
	People, Places []memory.Entity
	From, To       time.Time
	Expressions    []time.Time
	Natures        []string
}

func b2Library(t *testing.T, s *Store, scope memory.Scope, n int) b2LibraryFixture {
	t.Helper()
	g := b2LibraryFixture{}
	at := b2Anchor(t, "Asia/Shanghai")
	self := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Name: "我", Type: "self", Aliases: []string{"我"}}
	entities := []memory.Entity{self}
	for _, spec := range []struct{ name, kind string }{{"老王", "person"}, {"张三", "person"}, {"小李", "person"}, {"成都", "place"}, {"大理", "place"}} {
		e := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Name: spec.name, Type: spec.kind, Aliases: []string{spec.name}}
		entities = append(entities, e)
		if spec.kind == "person" {
			g.People = append(g.People, e)
		} else {
			g.Places = append(g.Places, e)
		}
	}
	req := memory.CommitRequest{RequestID: memory.NewID(), Entities: entities}
	g.From, g.To = b2Interval(t, "Asia/Shanghai", "range")
	for x := 0; x < n; x++ {
		date := at.AddDate(0, 0, -x)
		nature := []string{"fact", "preference", "intention", "plan", "decision"}[x%5]
		ref := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
		req.Claims = append(req.Claims, memory.Claim{Revision: memory.Revision{Ref: ref, ExpressedAt: &date}, SubjectID: self.ID, Predicate: fmt.Sprintf("fixture-%03d", x), Value: asJSON(b2Label(x)), Nature: nature, Acquisition: "direct", Confirmation: "adopted"})
		g.Refs = append(g.Refs, ref)
		g.Expressions = append(g.Expressions, date)
		g.Natures = append(g.Natures, nature)
	}
	if _, err := s.Commit(context.Background(), scope, req); err != nil {
		t.Fatal(err)
	}
	for x, ref := range g.Refs {
		b2Exec(t, s, `UPDATE record_versions SET actor='ai' WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), string(ref.ID))
		b2Exec(t, s, `UPDATE memory_records SET updated_at=$1 WHERE owner_id=$2 AND id=$3`, at.Add(time.Duration(x)*time.Minute), string(scope.OwnerID), string(ref.ID))
		b2Exec(t, s, `UPDATE claim_revisions SET event_from=$1,event_to=$2,event_precision='range' WHERE owner_id=$3 AND claim_id=$4`, g.From, g.To, string(scope.OwnerID), string(ref.ID))
		for _, e := range []memory.Entity{g.People[x%len(g.People)], g.Places[x%len(g.Places)]} {
			role := e.Type
			b2Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4)`, string(scope.OwnerID), string(ref.ID), string(e.ID), role)
		}
		workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(ref.ID), AgentIDs: []string{"model", "manual"}})
	}
	return g
}
func TestPhase2B2_M1_FiltersCursorIsolationAndOwnership(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, `{"reply":"合成回答","actions":[]}`)
	g := b2Library(t, s, scope, b2Want[int](t, "M1", "initial_total"))
	cases := []struct {
		name, query string
		include     func(int) bool
	}{
		{"person", "?entity=" + string(g.People[0].ID), func(i int) bool { return i%3 == 0 }},
		{"place", "?entity=" + string(g.Places[1].ID), func(i int) bool { return i%2 == 1 }},
		{"nature", "?nature=plan", func(i int) bool { return g.Natures[i] == "plan" }},
		{"time", "?from=" + url.QueryEscape(g.Expressions[90].Add(-30*time.Minute).Format(time.RFC3339)) + "&to=" + url.QueryEscape(g.Expressions[20].Add(30*time.Minute).Format(time.RFC3339)), func(i int) bool { return i >= 20 && i <= 90 }},
		{"text", "?q=" + url.QueryEscape("合成记忆012"), func(i int) bool { return i == 12 }},
		{"intersection", "?entity=" + string(g.People[0].ID) + "&nature=plan", func(i int) bool { return i%3 == 0 && g.Natures[i] == "plan" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := []string{}
			for i, r := range g.Refs {
				if tc.include(i) {
					want = append(want, string(r.ID))
				}
			}
			sort.Strings(want)
			got := b2List(t, s, scope, tc.query+"&limit=100")
			b2Equal(t, got.Total, len(want))
			b2Equal(t, b2MemoryIDs(got.Items), want)
			for _, m := range got.Items {
				b2Mentions(t, m)
				b2Event(t, m, &g.From, &g.To, "range")
			}
		})
	}
	first := b2List(t, s, scope, "?limit=50")
	b2Equal(t, len(first.Items), 50)
	if first.Next == "" {
		t.Fatal("first page lacks cursor")
	}
	src := b2Source(t, s, scope, "memory-input", "user", "翻页期间新记忆", nil)
	newRef := b1Claim(t, s, scope, "翻页期间新记忆", "fact", "adopted", src)
	b2Exec(t, s, `UPDATE memory_records SET updated_at=now()+interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(newRef.ID))
	seen := b2MemoryIDs(first.Items)
	cursor := first.Next
	for pageNo := 1; pageNo < 3; pageNo++ {
		page := b2List(t, s, scope, "?limit=50&cursor="+url.QueryEscape(cursor))
		b2Equal(t, len(page.Items), b2Want[[]int](t, "M1", "page_sizes")[pageNo])
		seen = append(seen, b2MemoryIDs(page.Items)...)
		cursor = page.Next
	}
	b2Equal(t, cursor, "")
	sort.Strings(seen)
	want := []string{}
	for _, r := range g.Refs {
		want = append(want, string(r.ID))
	}
	sort.Strings(want)
	b2Equal(t, seen, want)
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Error("cursor duplicated", seen[i])
		}
	}
	for _, id := range seen {
		if id == string(newRef.ID) {
			t.Error("new row shifted stable scan")
		}
	}
	b2Equal(t, b2List(t, s, scope, "").Total, 131)
	b2Equal(t, len(b2List(t, s, scope, "").Items), 50)
	b2Equal(t, len(b2List(t, s, scope, "?limit=1000").Items), 100)
	detail := b2Detail(t, s, scope, string(g.Refs[0].ID))
	b2Time(t, detail, "expressedAt", &g.Expressions[0])
	b2Names(t, detail, "person", []string{"老王"})
	outsider := scope
	outsider.IsOwner = false
	outsider.PrincipalID = "non-owner"
	for _, path := range []string{"/v1/workspace/memories", "/v1/workspace/memories/" + detail.ID, "/v1/workspace/memory-facets"} {
		w := b1HTTP(t, s, outsider, "GET", path, nil)
		if w.Code != 403 {
			t.Errorf("owner boundary %s got %d", path, w.Code)
		}
	}
	other := owner()
	b2Equal(t, b2List(t, s, other, "").Total, 0)
	w := b1HTTP(t, s, other, "GET", "/v1/workspace/memories/"+detail.ID, nil)
	b2Equal(t, w.Code, 404)
	b1Delete(t, s, scope, g.Refs[0])
	w = b1HTTP(t, s, scope, "GET", "/v1/workspace/memories/"+detail.ID, nil)
	b2Equal(t, w.Code, 404)
}
func TestPhase2B2_M2_SnapshotCapDoesNotCapSecretaryOrDeputy(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, `{"reply":"回忆结果","used":["M1"],"actions":[]}`)
	g := b2Library(t, s, scope, b2Want[int](t, "M2", "total"))
	oldest := g.Refs[0]
	b2Exec(t, s, `UPDATE claim_revisions SET value=$1 WHERE owner_id=$2 AND claim_id=$3`, asJSON("成都旧记忆唯一蓝色灯塔5271"), string(scope.OwnerID), string(oldest.ID))
	st := b2Snapshot(t, s, scope)
	b2Equal(t, len(st.Memories), b2Want[int](t, "M2", "snapshot_size"))
	total, ok := b1Map(t, st)["memoryTotal"].(float64)
	if !ok {
		t.Error("snapshot missing numeric memoryTotal")
	} else {
		b2Equal(t, int(total), b2Want[int](t, "M2", "total"))
	}
	for _, m := range st.Memories {
		if m.ID == string(oldest.ID) {
			t.Error("fixture oldest unexpectedly in snapshot")
		}
	}
	req := turnRequest("成都旧记忆唯一蓝色灯塔5271是什么")
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, "成都旧记忆唯一蓝色灯塔5271")
	f.set("副手回忆结果")
	run := b1Run(t, s, scope, "model", "成都旧记忆唯一蓝色灯塔5271")
	b1Contains(t, f.last(t).Prompt, "成都旧记忆唯一蓝色灯塔5271")
	b1HasRef(t, run.ContextVersions, oldest, true)
}
func TestPhase2B2_M3_FacetCountsDeletionAndFiftyLimit(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	g := b2Library(t, s, scope, 4)
	b2Exec(t, s, `UPDATE claim_mentions SET entity_id=$1 WHERE owner_id=$2 AND claim_id=$3 AND role='place'`, string(g.Places[0].ID), string(scope.OwnerID), string(g.Refs[3].ID))
	b1Delete(t, s, scope, g.Refs[2])
	w := b1HTTP(t, s, scope, "GET", "/v1/workspace/memory-facets", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		People, Places []struct {
			EntityID string `json:"entityId"`
			Name     string `json:"name"`
			Count    int    `json:"count"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		role  string
		items []struct {
			EntityID string `json:"entityId"`
			Name     string `json:"name"`
			Count    int    `json:"count"`
		}
	}{{"people", out.People}, {"places", out.Places}} {
		actual := map[string]int{}
		for _, x := range tc.items {
			if x.EntityID == "" {
				t.Error("facet missing entityId")
			}
			actual[x.Name] = x.Count
		}
		b2Equal(t, actual, b2Want[map[string]int](t, "M3", tc.role))
	}
}
func b2AnsweredPlan(t *testing.T) (*Store, memory.Scope, *b1Fake, workspace.DeskTurnResponse, workspace.DeskTurnResponse, memory.Ref, workspace.Run) {
	t.Helper()
	s, scope, f, _, created, src := b1Plan(t, b1CreatePlan)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b2Extract(t, s, scope, f, src, b2Item(text, text, "plan")), text)
	f.set(`{"reply":"张三方案计划的合成回答6193","used":["M1"],"actions":[]}`)
	req := turnRequest("张三方案计划是什么")
	answer := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, true)
	f.set("张三方案副手合成回答6193")
	run := b1Run(t, s, scope, "model", "张三方案计划是什么")
	b1HasRef(t, run.ContextVersions, claim, true)
	return s, scope, f, created, answer, claim, run
}
func TestPhase2B2_M4_UndoPreservesAnswerAndDeputyReplacesModelHistory(t *testing.T) {
	s, scope, f, created, answered, claim, run := b2AnsweredPlan(t)
	before := b1History(t, s, scope, answered.ConversationID)
	st := b1Undo(t, s, scope, b1ReceiptAction(t, created.Turn, 0))
	b1Active(t, s, scope, claim, false)
	after := b1History(t, s, scope, answered.ConversationID)
	b1Preserved(t, before, after)
	found := false
	for _, r := range st.Runs {
		if r.ID == run.ID {
			found = true
			b2Equal(t, r.Output, run.Output)
			b2Equal(t, r.StaleContext, true)
		}
	}
	if !found {
		t.Error("undo erased dependent deputy run")
	}
	replacement := ""
	b2Supplement(t, "history_replacement", &replacement)
	f.set(`{"reply":"按当前资料回答","actions":[]}`)
	req := turnRequest("接着说张三方案")
	req.ConversationID = &answered.ConversationID
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, answered.Turn.Text, replacement)
	b1Absent(t, f.last(t).Prompt, answered.Turn.Reply)
	b1Outdated(t, b1History(t, s, scope, created.ConversationID), false)
}
func TestPhase2B2_M5_ExplicitDeleteStillClearsDependents(t *testing.T) {
	s, scope, _, _, answered, claim, run := b2AnsweredPlan(t)
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: string(claim.ID)})
	b1Active(t, s, scope, claim, false)
	history := b1History(t, s, scope, answered.ConversationID)
	b1Absent(t, history.Reply, answered.Turn.Reply)
	b2Equal(t, len(history.Cards), 0)
	st := b2Snapshot(t, s, scope)
	for _, r := range st.Runs {
		if r.ID == run.ID {
			b1Absent(t, r.Output, run.Output)
			b2Equal(t, r.Output, "")
		}
	}
}
func TestPhase2B2_M7_NewDeputyVisibilityInitializesOnlyOnce(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, "合成副手回答")
	g := b2Library(t, s, scope, 3)
	for n, r := range g.Refs {
		agents := []string{"model", "manual"}
		if n == 1 {
			agents = []string{"model"}
		}
		if n == 2 {
			agents = []string{}
		}
		workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(r.ID), AgentIDs: agents})
	}
	// Existing enabled agents have been observed before the new deputy appears.
	initial := b2Snapshot(t, s, scope)
	newAgent := initial.Agents[0]
	newAgent.ID = "B"
	newAgent.Name = "新副手B"
	newAgent.Enabled = false
	b2Exec(t, s, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,'B',$2)`, string(scope.OwnerID), asJSON(newAgent))
	registry := s.models
	registry.Config.Providers = append(registry.Config.Providers, ai.Provider{ID: "B", Name: "新副手B", Protocol: "openai", BaseURL: registry.Config.Providers[0].BaseURL, Model: "test", MaxOutput: 100, CostMode: "free"})
	s.SetModels(registry)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "B", Patch: asJSON(map[string]any{"enabled": true})})
	check := func(want []int) {
		t.Helper()
		st := b2Snapshot(t, s, scope)
		got := []int{}
		for n, r := range g.Refs {
			for _, m := range st.Memories {
				if m.ID == string(r.ID) {
					for _, a := range m.VisibleTo {
						if a == "B" {
							got = append(got, n)
						}
					}
				}
			}
		}
		b2Equal(t, got, want)
	}
	check([]int{0, 1})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(g.Refs[0].ID), AgentIDs: []string{"model", "manual"}})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "B", Patch: asJSON(map[string]any{"enabled": false})})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "B", Patch: asJSON(map[string]any{"enabled": true})})
	check([]int{1})
}
func TestPhase2B2_M8_DeleteCleansOrphansButKeepsSharedAndSelf(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	at := b2Anchor(t, "Asia/Shanghai")
	text := "我去成都见老王"
	src := b2Source(t, s, scope, "capture", "user", text, &at)
	i := b2Item(text, text, "fact")
	i["people"] = []string{"老王"}
	i["places"] = []string{"成都"}
	m := b2One(t, b2Extract(t, s, scope, f, src, i))
	self, _ := b2Subject(t, s, scope, m)
	person, place := b2MentionID(t, m, "老王", "person"), b2MentionID(t, m, "成都", "place")
	next := b2Source(t, s, scope, "capture", "user", "我喜欢成都火锅", &at)
	j := b2Item("我喜欢成都火锅", "我喜欢成都火锅", "preference")
	j["places"] = []string{"成都"}
	b2Extract(t, s, scope, f, next, j)
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: m.ID})
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entities WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), person), 0)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM aliases WHERE owner_id=$1 AND entity_id=$2`, string(scope.OwnerID), person), 0)
	for _, id := range []string{place, self} {
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entities WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id), 1)
	}
}
func TestPhase2B2_M9_CorrectionCarriesMentionsAndEventToNewVersion(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	g := b2Library(t, s, scope, 1)
	before := b2Detail(t, s, scope, string(g.Refs[0].ID))
	b1Correct(t, s, scope, g.Refs[0], "用户修订后的合成记忆")
	after := b2Detail(t, s, scope, before.ID)
	b2Equal(t, after.Version, b2Want[int](t, "M9", "version"))
	b2Equal(t, b2Mentions(t, after), b2Mentions(t, before))
	b2Event(t, after, &g.From, &g.To, "range")
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=1`, string(scope.OwnerID), before.ID), 2)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=2`, string(scope.OwnerID), before.ID), 2)
}
func TestPhase2B2_M10_DistinctActionableHumanJobMessages(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	codes := b2Want[[]string](t, "M10", "codes")
	refs := []memory.Ref{}
	for n, code := range codes {
		src := b2Source(t, s, scope, "desk", "user", b2Label(n), nil)
		b2OnlyExtraction(t, s, scope, src, 0)
		state := "blocked"
		if code == "budget_deferred" {
			state = "queued"
		}
		b2Exec(t, s, `UPDATE memory_jobs SET state=$1,error_code=$2,available_at=now()+interval '1 day' WHERE owner_id=$3 AND record_id=$4 AND stage='source.extract'`, state, code, string(scope.OwnerID), string(src.ID))
		refs = append(refs, src)
	}
	st := b2Snapshot(t, s, scope)
	messages := []string{}
	for _, ref := range refs {
		found := false
		for _, j := range st.Jobs {
			if strings.HasSuffix(j.ID, b2SQLIDs(t, s, `SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(scope.OwnerID), string(ref.ID))[0]) {
				found = true
				text := j.Detail + " " + j.Recovery
				if j.Recovery == "" {
					t.Error("job must give a recovery suggestion", j.ID)
				}
				if len([]rune(text)) < 8 {
					t.Error("job explanation lacks an actionable sentence", text)
				}
				b1Absent(t, text, codes...)
				messages = append(messages, text)
			}
		}
		if !found {
			t.Error("job missing from unfinished-work notices", ref)
		}
	}
	if len(messages) == 2 && messages[0] == messages[1] {
		t.Error("unavailable and deferred have same explanation")
	}
}
func TestPhase2B2_M11_ExportContainsNewTablesAndEventValues(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	g := b2Library(t, s, scope, 1)
	// Populate every newly exported table, so an empty placeholder isn't enough.
	src := b2Source(t, s, scope, "archive", "user", "合成导出原话", nil)
	batch, usage := string(memory.NewID()), string(memory.NewID())
	at := b2Anchor(t, "Asia/Shanghai")
	b2Exec(t, s, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items) VALUES($1,$2,1,2,'done',1)`, string(scope.OwnerID), string(src.ID))
	b2Exec(t, s, `INSERT INTO import_batches(owner_id,id,archive_id,name,state,total,stored) VALUES($1,$2,$3,'合成导出批次','done',1,1)`, string(scope.OwnerID), batch, string(src.ID))
	b2Exec(t, s, `INSERT INTO model_usage(owner_id,id,at,purpose,model,input_tokens,output_tokens,cost,memory_refs,plan) VALUES($1,$2,$3,'extraction','synthetic',100,20,0,$4,'{}')`, string(scope.OwnerID), usage, at, asJSON([]memory.Ref{g.Refs[0]}))
	data, err := s.Export(context.Background(), scope, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var export any
	if err = json.Unmarshal(data, &export); err != nil {
		t.Fatal(err)
	}
	for _, table := range b2Want[[]string](t, "M11", "tables") {
		if _, ok := b2ExportRows(export, table); !ok {
			t.Error("export missing raw table", table)
		}
	}
	for _, col := range b2Want[[]string](t, "M11", "event_columns") {
		b1Contains(t, string(data), `"`+col+`"`)
	}
	b1Contains(t, string(data), string(g.People[0].ID), string(g.Places[0].ID), "range", batch, usage, string(src.ID))
	revisions, ok := b2ExportRows(export, "claim_revisions")
	if !ok {
		t.Fatal("export missing claim revisions")
	}
	found := false
	for _, raw := range revisions {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("export row is not an object")
		}
		if row["claim_id"] == string(g.Refs[0].ID) {
			found = true
			for key, want := range map[string]time.Time{"event_from": g.From, "event_to": g.To} {
				raw, ok := row[key].(string)
				if !ok {
					t.Error("export lost event", key)
					continue
				}
				got, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil || !got.Equal(want) {
					t.Error("export event differs", key, raw)
				}
			}
		}
	}
	if !found {
		t.Error("export missing fixture revision")
	}
}

func TestPhase2B2_M6_AllBatch1SequencesUnchanged(t *testing.T) {
	t.Run("G1_FreshDatabaseMigratesWithoutLegacyObjects", TestPhase2B1_G1_FreshDatabaseMigratesWithoutLegacyObjects)
	t.Run("G2_LegacyCleanupPreservesEveryBusinessRow", TestPhase2B1_G2_LegacyCleanupPreservesEveryBusinessRow)
	t.Run("G3_SecondStartupMakesNoFurtherChanges", TestPhase2B1_G3_SecondStartupMakesNoFurtherChanges)
	t.Run("M1_ClaimCorrectionKeepsVisibleExchange", TestPhase2B1_M1_ClaimCorrectionKeepsVisibleExchange)
	t.Run("M2_ModelHistoryReplacesOnlyOldAnswer", TestPhase2B1_M2_ModelHistoryReplacesOnlyOldAnswer)
	t.Run("M3_SourceVersionMarksOutdatedWithoutErasure", TestPhase2B1_M3_SourceVersionMarksOutdatedWithoutErasure)
	t.Run("M4_SourceDeletionScrubsDependentTurnRunAndDoc", TestPhase2B1_M4_SourceDeletionScrubsDependentTurnRunAndDoc)
	t.Run("M5_OutdatedReceiptCanUndoAndSurvivesReload", TestPhase2B1_M5_OutdatedReceiptCanUndoAndSurvivesReload)
	t.Run("M6_ReplayAndTelegramDuplicateKeepOldAnswer", TestPhase2B1_M6_ReplayAndTelegramDuplicateKeepOldAnswer)
	t.Run("M7_CorrectedDeputyResultRemainsButStale", TestPhase2B1_M7_CorrectedDeputyResultRemainsButStale)
	t.Run("M8_UnchangedTurnOmitsOutdatedField", TestPhase2B1_M8_UnchangedTurnOmitsOutdatedField)
	t.Run("M2_CurrentDestinationUnavailablePreservesQuestionAndReceipt", TestPhase2B1_M2_CurrentDestinationUnavailablePreservesQuestionAndReceipt)
	t.Run("P1_DeskTurnRawSourceAndDeletionControl", TestPhase2B1_P1_DeskTurnRawSourceAndDeletionControl)
	t.Run("P2_ActualDeputyRequestAndSourceDependencies", TestPhase2B1_P2_ActualDeputyRequestAndSourceDependencies)
	t.Run("P3_ManualBriefAndDeletionControl", TestPhase2B1_P3_ManualBriefAndDeletionControl)
	t.Run("P4_ClaimOnlyEmptySourceSection", TestPhase2B1_P4_ClaimOnlyEmptySourceSection)
	t.Run("P5_SystemGeneratedSourcesExcluded", TestPhase2B1_P5_SystemGeneratedSourcesExcluded)
	t.Run("P6_BudgetsTruncateAndComplete", TestPhase2B1_P6_BudgetsTruncateAndComplete)
	t.Run("P7_HistoricalQuestionsNotRepeatedAsSources", TestPhase2B1_P7_HistoricalQuestionsNotRepeatedAsSources)
	t.Run("P8_DuplicateTitlesDoNotMergeIdentities", TestPhase2B1_P8_DuplicateTitlesDoNotMergeIdentities)
	t.Run("P9_PublicReadsCannotForgeInternalAccess", TestPhase2B1_P9_PublicReadsCannotForgeInternalAccess)
	t.Run("P10_DeputyClaimFiltersAndSharedRawSources", TestPhase2B1_P10_DeputyClaimFiltersAndSharedRawSources)
	t.Run("P11_CurrentVersionOnly", TestPhase2B1_P11_CurrentVersionOnly)
	t.Run("P12_MediaRequiresReadableTranscriptOrOCR", TestPhase2B1_P12_MediaRequiresReadableTranscriptOrOCR)
	t.Run("P13_AdoptedSourceContentSurvivesForUserButExpiresForModels", TestPhase2B1_P13_AdoptedSourceContentSurvivesForUserButExpiresForModels)
	t.Run("P6_MatchedTailExcerptBeforeAndAfterChunking", TestPhase2B1_P6_MatchedTailExcerptBeforeAndAfterChunking)
	t.Run("P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly", TestPhase2B1_P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly)
	t.Run("P14_CapturedMemoryVisibilityClosesAndReopensOriginal", TestPhase2B1_P14_CapturedMemoryVisibilityClosesAndReopensOriginal)
	t.Run("P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances", TestPhase2B1_P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances)
	t.Run("P14_OneRestrictedActiveClaimClosesWholeSource", TestPhase2B1_P14_OneRestrictedActiveClaimClosesWholeSource)
	t.Run("P15_SourceDependentAdoptionObeysVisibilityAndItemExclusion", TestPhase2B1_P15_SourceDependentAdoptionObeysVisibilityAndItemExclusion)
	t.Run("P3_BriefDateUsesWorkspaceTimezoneAcrossUTCDayBoundary", TestPhase2B1_P3_BriefDateUsesWorkspaceTimezoneAcrossUTCDayBoundary)
	t.Run("N1_UndoDeletesPlanKeepsOriginalAndHistory", TestPhase2B1_N1_UndoDeletesPlanKeepsOriginalAndHistory)
	t.Run("N2_ExtractionAfterFullUndoDoesNotRecreatePlan", TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan)
	t.Run("N3_PartialUndoKeepsMemoryAndRawSupply", TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply)
	t.Run("N4_RememberKeepsPreferenceButDeletesPlan", TestPhase2B1_N4_RememberKeepsPreferenceButDeletesPlan)
	t.Run("N5_ConfirmedClaimSurvivesFullUndo", TestPhase2B1_N5_ConfirmedClaimSurvivesFullUndo)
	t.Run("N6_IndependentEvidenceProtectsClaim", TestPhase2B1_N6_IndependentEvidenceProtectsClaim)
	t.Run("N7_RefusedUndoIsAtomic", TestPhase2B1_N7_RefusedUndoIsAtomic)
	t.Run("N8_TelegramCallbackUsesRealUndo", TestPhase2B1_N8_TelegramCallbackUsesRealUndo)
	t.Run("N9_UndoCommandReplayHasNoSecondDeletion", TestPhase2B1_N9_UndoCommandReplayHasNoSecondDeletion)
	t.Run("N10_ManualUndoDoesNotCascadeSecretaryMemory", TestPhase2B1_N10_ManualUndoDoesNotCascadeSecretaryMemory)
	t.Run("N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn", TestPhase2B1_N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn)
	t.Run("N12_RandomTwentyActionsInTwentySeededGroups", TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups)
}

func b2ExportRows(value any, table string) ([]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		if rows, ok := v[table].([]any); ok {
			return rows, true
		}
		for _, child := range v {
			if rows, ok := b2ExportRows(child, table); ok {
				return rows, true
			}
		}
	case []any:
		for _, child := range v {
			if rows, ok := b2ExportRows(child, table); ok {
				return rows, true
			}
		}
	}
	return nil, false
}

func TestPhase2B2_M3_FacetsTakeFiftyMostFrequentPerRole(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	g := b2Library(t, s, scope, 120)
	entities := []memory.Entity{}
	byRole := map[string][]memory.Entity{}
	for _, role := range []string{"person", "place"} {
		for n := 0; n < 60; n++ {
			e := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Type: role, Name: fmt.Sprintf("%s合成%02d", role, n)}
			entities = append(entities, e)
			byRole[role] = append(byRole[role], e)
		}
	}
	if _, err := s.Commit(context.Background(), scope, memory.CommitRequest{RequestID: memory.NewID(), Entities: entities}); err != nil {
		t.Fatal(err)
	}
	b2Exec(t, s, `DELETE FROM claim_mentions WHERE owner_id=$1`, string(scope.OwnerID))
	for n, ref := range g.Refs {
		for role, values := range byRole {
			b2Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4)`, string(scope.OwnerID), string(ref.ID), string(values[n%60].ID), role)
		}
	}
	for role, values := range byRole {
		b2Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4)`, string(scope.OwnerID), string(g.Refs[1].ID), string(values[0].ID), role)
	}
	w := b1HTTP(t, s, scope, "GET", "/v1/workspace/memory-facets", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string][]struct {
		EntityID string `json:"entityId"`
		Count    int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for key, role := range map[string]string{"people": "person", "places": "place"} {
		items := out[key]
		b2Equal(t, len(items), b2Want[int](t, "M3", "max_per_role"))
		seen := map[string]bool{}
		foundHighest := false
		previous := 1000
		for _, item := range items {
			if seen[item.EntityID] {
				t.Error("facet duplicated entity")
			}
			seen[item.EntityID] = true
			if item.Count > previous {
				t.Error("facets not ranked by memory count")
			}
			previous = item.Count
			if item.EntityID == string(byRole[role][0].ID) {
				foundHighest = true
				b2Equal(t, item.Count, 3)
			} else {
				b2Equal(t, item.Count, 2)
			}
		}
		if !foundHighest {
			t.Error("top facet omitted")
		}
	}
}
