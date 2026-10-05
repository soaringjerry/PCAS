package postgres_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (f *phase25B1Fixture) project(t *testing.T, title, status string) memory.ID {
	t.Helper()
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	id := memory.NewID()
	data, err := json.Marshal(workspace.Item{ID: string(id), Kind: "project", Version: 1, Title: title, Name: title, Status: status, Body: "虚构项目说明"})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO work_items(owner_id,id,kind,title,status,version,document,created_at,updated_at) VALUES($1,$2,'project',$3,$4,1,$5,now(),now())`, f.scope.OwnerID, id, title, status, data)
	return id
}

func TestPhase25B1_X02_DependentAnswerAndAgentRunRemainCurrent(t *testing.T) {
	f := phase25B1NewFixture(t)
	f.scope.Team = true
	refs := []memory.Ref{f.claim(t, "虚构规则 AcceptanceTitle 青色标题。"), f.claim(t, "虚构规则 AcceptanceTitle 先列结论。"), f.claim(t, "虚构规则 AcceptanceTitle 三张插图。")}
	project := f.project(t, "虚构季度汇报", "active")
	reply, err := json.Marshal(map[string]any{"answer": "虚构回答：青色标题、结论和插图。", "used": []string{string(refs[0].ID), string(refs[1].ID), string(refs[2].ID)}})
	if err != nil {
		t.Fatal(err)
	}
	f.model(t, string(reply))
	agentID := "phase25-b1-fake"
	routing, err := json.Marshal(workspace.Agent{ID: agentID, Name: "虚构副手", Channel: agentID, Enabled: true, MemoryInitialized: true, MemoryKinds: []string{"fact", "preference", "plan", "decision", "intention"}})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,$2,$3) ON CONFLICT(owner_id,id) DO UPDATE SET document=excluded.document`, f.scope.OwnerID, agentID, routing)
	// This manually registered fixture agent is already marked initialized;
	// give it the fictitious records that its dependency-producing API reads.
	for _, principal := range []string{agentID, "agent:" + agentID} {
		f.exec(t, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,$2 FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.scope.OwnerID, principal)
	}
	for i := 0; i < 20; i++ {
		j, err := f.store.ClaimIndex(f.ctx, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			break
		}
		if err := f.store.ProcessIndex(f.ctx, *j); err != nil {
			t.Fatal(err)
		}
	}
	answer, err := f.store.AnswerDesk(f.ctx, f.scope, agentID, "AcceptanceTitle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Used) != 3 {
		t.Fatalf("dependent answer used %d memories, want 3", len(answer.Used))
	}
	conversation, turnID, runID := memory.NewID(), memory.ID(answer.ID), memory.NewID()
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	turnJSON, err := json.Marshal(workspace.DeskTurnResponse{ConversationID: string(conversation), Turn: workspace.SecretaryTurn{ID: string(turnID), Text: "AcceptanceTitle", Reply: answer.Answer, Agent: agentID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, State: state})
	if err != nil {
		t.Fatal(err)
	}
	// Attach the real stored answer to the current conversation read interface;
	// its dependency payload is created by AnswerDesk, never guessed here.
	f.exec(t, `UPDATE desk_turns SET conversation_id=$3,response=$4,request_id=$5 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, turnID, conversation, turnJSON, memory.NewID())
	agentJSON, err := json.Marshal(workspace.Agent{ID: "fictitious-agent", Name: "虚构副手", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,'fictitious-agent',$2) ON CONFLICT(owner_id,id) DO NOTHING`, f.scope.OwnerID, agentJSON)
	runJSON, err := json.Marshal(workspace.Run{ID: string(runID), ThingID: string(project), AgentID: "fictitious-agent", Kind: "draft", Status: "done", Output: "虚构副手结果。", ContextVersions: refs, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) VALUES($1,$2,$3,'fictitious-agent','done',0,now(),$4)`, f.scope.OwnerID, runID, project, runJSON)
	for _, ref := range refs {
		f.exec(t, `INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4)`, f.scope.OwnerID, runID, ref.ID, ref.Version)
	}
	check := func(wantOutdated bool) {
		turns, err := f.store.DeskTurns(f.ctx, f.scope, string(conversation))
		if err != nil {
			t.Fatal(err)
		}
		if len(turns.Turns) != 1 || turns.Turns[0].ID != string(turnID) {
			t.Fatalf("dependent turns = %+v", turns.Turns)
		}
		if turns.Turns[0].Outdated != wantOutdated {
			t.Errorf("answer outdated = %v, want %v", turns.Turns[0].Outdated, wantOutdated)
		}
		var state workspace.State
		f.get(t, "/v1/workspace", &state)
		found := false
		for _, run := range state.Runs {
			if run.ID == string(runID) {
				found = true
				if (!wantOutdated && (run.Outdated || run.StaleContext)) || (wantOutdated && !run.Outdated && !run.StaleContext) {
					t.Errorf("agent result outdated/stale = %v/%v, want %v", run.Outdated, run.StaleContext, wantOutdated)
				}
			}
		}
		if !found {
			t.Fatal("dependent run missing")
		}
	}
	check(false)
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(3, "rule", true)))
	f.batch(t)
	check(false)
	// Positive control: a real correction must make these dependencies stale.
	if _, err := f.correction(refs[0], "虚构季度汇报改用紫色标题。"); err != nil {
		t.Fatal(err)
	}
	check(true)
}

func TestPhase25B1_X13_DirectMemoryBeforeImportedMemories(t *testing.T) {
	f := phase25B1NewFixture(t)
	direct := f.claim(t, "虚构直接输入便签，虽较旧也优先整理。")
	// Construct a canonical imported-source fixture. Archive decoding/extraction
	// is outside this batch; origin membership is persisted in archive_entries.
	archive, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{Connector: "archive", ExternalID: "fictitious-archive", Title: "虚构归档包", Text: "虚构归档包说明。"})
	if err != nil {
		t.Fatal(err)
	}
	importedSource, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{Connector: "chatgpt", ExternalID: "fictitious-archived-conversation", Title: "虚构归档对话", Text: "虚构归档内容。"})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO archive_entries(owner_id,archive_id,archive_version,source_id,source_version) VALUES($1,$2,$3,$4,$5)`, f.scope.OwnerID, archive.ID, archive.Version, importedSource.ID, importedSource.Version)
	source := importedSource.Ref
	subject := f.entity(t, "person", "虚构归档人物")
	imported := make([]memory.Ref, 100)
	for i := range imported {
		ref := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
		imported[i] = ref
		evidence := memory.NewID()
		value, err := json.Marshal(fmt.Sprintf("虚构归档便签第%d条。", i))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.store.Commit(f.ctx, f.scope, memory.CommitRequest{RequestID: memory.NewID(), Claims: []memory.Claim{{Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: subject, Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "confirmed", Evidence: []memory.ID{evidence}}}, Evidence: []memory.Evidence{{ID: evidence, Source: source, Target: ref, Acquisition: "direct", Stance: "supports"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(40, "identity", true)))
	f.batch(t)
	batches := f.usages(t)
	if len(batches) != 1 || len(batches[0]) != 40 {
		t.Fatalf("first batch = %+v", batches)
	}
	selected := make(map[memory.ID]bool)
	for _, ref := range batches[0] {
		selected[ref.ID] = true
	}
	if !selected[direct.ID] {
		t.Error("older direct memory absent from first batch")
	}
	for i, ref := range imported {
		want := i >= 61
		if selected[ref.ID] != want {
			t.Errorf("imported selection %d = %v, want %v", i, selected[ref.ID], want)
		}
	}
}

func TestPhase25B1_X19_SeedProjectsAndElevenAreasIdempotently(t *testing.T) {
	f := phase25B1NewFixture(t)
	first := f.project(t, "虚构灯塔计划", "active")
	second := f.project(t, "虚构航模计划", "paused")
	completed := f.project(t, "虚构已完成计划", "done")
	f.claim(t, "虚构人物正在安排项目日程。")
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(40, "progress", false)))
	f.batch(t)
	check := func(wantProjects int) {
		rows, err := f.db.Query(f.ctx, `SELECT entity_type,name,coalesce(disambiguation->>'work_item_id','') FROM entity_versions WHERE owner_id=$1`, f.scope.OwnerID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		areas := make(map[string]int)
		projects := make(map[string]int)
		for rows.Next() {
			var typ, name, workID string
			if err := rows.Scan(&typ, &name, &workID); err != nil {
				t.Fatal(err)
			}
			if typ == "area" {
				areas[name]++
			}
			if typ == "project" {
				projects[workID]++
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(areas) != 11 {
			t.Errorf("areas = %+v", areas)
		}
		for _, name := range []string{"学业", "工作", "创业", "技术", "健康", "财务", "居住", "饮食", "出行", "关系", "兴趣"} {
			if areas[name] != 1 {
				t.Errorf("area %s = %d", name, areas[name])
			}
		}
		if len(projects) != wantProjects || projects[string(first)] != 1 || projects[string(second)] != 1 || projects[string(completed)] != 0 {
			t.Errorf("projects = %+v", projects)
		}
	}
	check(2)
	f.claim(t, "虚构另一条日程便签。")
	f.batch(t)
	check(2)
	third := f.project(t, "虚构纸船计划", "active")
	f.claim(t, "虚构新建事项项目的便签。")
	f.batch(t)
	check(3)
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='project' AND disambiguation->>'work_item_id'=$2`, f.scope.OwnerID, string(third)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("later project entity count = %d", count)
	}
}

func TestPhase25B1_X20_ProjectGroupDoesNotNarrowRetrieval(t *testing.T) {
	f := phase25B1NewFixture(t)
	projectA := f.project(t, "虚构季度汇报", "active")
	projectB := f.project(t, "虚构航模制作", "active")
	ref := f.claim(t, "虚构季度汇报要求使用青色标题。")
	// Populate the real lexical index through its queued processing entrypoint.
	for i := 0; i < 20; i++ {
		j, err := f.store.ClaimIndex(f.ctx, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			break
		}
		if err := f.store.ProcessIndex(f.ctx, *j); err != nil {
			t.Fatal(err)
		}
	}
	var originalScope string
	if err := f.db.QueryRow(f.ctx, `SELECT scope::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&originalScope); err != nil {
		t.Fatal(err)
	}
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "rule", Durable: true, Project: "虚构季度汇报"}}))
	f.batch(t)
	var scope string
	if err := f.db.QueryRow(f.ctx, `SELECT scope::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&scope); err != nil {
		t.Fatal(err)
	}
	if scope != originalScope || strings.Contains(scope, "project_id") {
		t.Errorf("scope changed: %s -> %s", originalScope, scope)
	}
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "虚构季度汇报" {
		t.Errorf("project grouping = %+v", m.Groups)
	}
	for _, project := range []memory.ID{projectA, projectB} {
		result, err := f.store.Recall(f.ctx, f.scope, memory.RecallRequest{Mode: memory.Continue, Query: "青色标题", Context: memory.WorkingContext{Objects: []memory.ID{project}}, Budget: memory.Budget{Candidates: 40, Edges: 40, Tokens: 4000, Hops: 2}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, memoryRef := range result.Memories {
			if memoryRef.ID == ref.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("grouped global memory absent from project %s retrieval: %+v", project, result.Memories)
		}
	}
}
