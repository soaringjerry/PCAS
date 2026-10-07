package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase3S1DefaultProjectAndS2ConversationInDeputy(t *testing.T) {
	phase3Finding(t, "S-P3-003")
	f := phase3LoadFixture(t)
	m := phase3NewModel(t, f)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage == "secretary" {
			return `{"reply":"已建虚构事项","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[{"op":"create_task","title":"虚构秘书默认项目事项"}],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`
		}
		return out
	}
	m.mu.Unlock()
	code, raw, err := h.call(f.Context, "POST", "/v1/desk/turn", workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase3", ThingID: &p.ID, Text: "预算采用17虚构单位，请建一个核对事项。"})
	if err != nil || code != 200 {
		t.Fatal(code, string(raw), err)
	}
	var turn workspace.DeskTurnResponse
	decodePhase3(t, raw, &turn)
	target := ""
	for _, it := range turn.State.Tasks {
		if it.Title == "虚构秘书默认项目事项" {
			if it.ProjectID != p.ID {
				t.Fatal("studio item did not inherit current project", it)
			}
			target = it.ID
		}
	}
	if target == "" {
		t.Fatal("secretary did not create expected task")
	}
	state := h.command(t, f.Context, map[string]any{"type": "requestRun", "thingId": target, "agentId": "manual", "kind": "draft", "prompt": "按刚才的虚构预算做一份方案。", "deskTurnIds": []string{turn.Turn.ID}})
	found := false
	for _, run := range state.Runs {
		if run.ThingID == target {
			found = true
			if !strings.Contains(run.Brief, "17虚构单位") {
				t.Fatal("deputy lost selected conversation context", run.Brief)
			}
		}
	}
	if !found {
		t.Fatal("missing deputy work")
	}
}
func TestPhase3H5H8S3ActualVersionAndHandoverProvenance(t *testing.T) {
	phase3Finding(t, "S-P3-003")
	f := phase3LoadFixture(t)
	m := phase3NewModel(t, f)
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	phase26Isolate(t, f.phase26LoadedFixture, HandoverStage)
	if _, err := phase26Schedule(f.Context, f.Store, HandoverStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f.phase26LoadedFixture, HandoverStage)
	globalJob := phase26ClaimStage(t, f.phase26LoadedFixture, HandoverStage)
	if err := phase26Process(f.Context, f.Store, globalJob); err != nil {
		t.Fatal(err)
	}
	global, err := f.Store.ReadHandover(f.Context, f.Scope)
	if err != nil || global.Body == "" {
		t.Fatal("missing actual global handover precondition", err)
	}
	phase3OnlyProject(t, f, f.Gold.Projects[0].ID)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	d := p.Documents[0]
	var handover phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &handover)
	if handover.WrittenAt == nil {
		t.Fatal("no generated project handover")
	}
	state := h.command(t, f.Context, map[string]any{"type": "requestRun", "thingId": p.ID, "kind": "revise", "agentId": "manual", "prompt": "按项目交接改第二版虚构预算。", "documentId": d.ID, "baseVersion": 2})
	runID := ""
	for _, run := range state.Runs {
		if run.Kind == "revise" {
			runID = run.ID
			if !strings.Contains(run.Brief, d.Versions[1].Body) || !strings.Contains(run.Brief, handover.Conclusion[0].Text) || !strings.Contains(run.Brief, global.Body) {
				t.Fatal("deputy did not receive base full body/current project handover")
			}
		}
	}
	if runID == "" {
		t.Fatal("no revise work")
	}
	var snapshot json.RawMessage
	h.get(t, f.Context, "/v1/workspace", &snapshot)
	var wire struct {
		Runs []struct {
			ID               string
			DocumentVersions []struct {
				DocumentID string
				Version    int
			}
			ProjectHandoverWrittenAt *string
		}
	}
	decodePhase3(t, snapshot, &wire)
	for _, r := range wire.Runs {
		if r.ID == runID {
			if r.ProjectHandoverWrittenAt == nil || *r.ProjectHandoverWrittenAt != *handover.WrittenAt {
				t.Fatal("work record has no actual handover time")
			}
			found := false
			for _, v := range r.DocumentVersions {
				if v.DocumentID == d.ID && v.Version == 2 {
					found = true
				}
			}
			if !found {
				t.Fatal("work record has no actual v2 input")
			}
		}
	}
	code, raw, err := h.call(f.Context, "POST", "/v1/desk/turn", map[string]any{"requestId": string(memory.NewID()), "agentId": "phase3", "thingId": p.ID, "text": "请接着这个虚构项目说明现状。"})
	if err != nil || code != 200 {
		t.Fatal("actual studio secretary request", code, string(raw), err)
	}
	m.mu.Lock()
	calls := append([]phase26Call{}, m.Calls["secretary"]...)
	m.mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("no actual secretary model request")
	}
	for _, call := range calls {
		if !phase3PromptHas(call.Prompt, handover.Conclusion[0].Text) || !phase3PromptHas(call.Prompt, global.Body) || !strings.Contains(call.System+call.Prompt, "资料") || !strings.Contains(call.System+call.Prompt, "指令") {
			t.Fatal("secretary missing project/global handover or data boundary")
		}
	}
}
func TestPhase3H7AdoptProgressCreatesProjectMemoryAndUndo(t *testing.T) {
	phase3Finding(t, "S-P3-003")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[1]
	run := p.Runs[0]
	action := string(memory.NewID())
	// A finished, unadopted deputy result is an input fixture, not a model output oracle.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE agent_runs SET document=document-'adopted' WHERE owner_id=$1 AND id=$2`, f.Scope.OwnerID, run.ID)
	before, err := f.Store.Snapshot(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	h.command(t, f.Context, map[string]any{"type": "adoptRun", "id": run.ID, "as": "progress", "text": run.Output, "requestId": action})
	var n int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM claim_revisions c JOIN claims cl ON(cl.owner_id,cl.id)=(c.owner_id,c.claim_id) WHERE c.owner_id=$1 AND c.scope->>'project_id'=$2 AND c.value #>> '{}'=$3 AND cl.retired=''`, f.Scope.OwnerID, p.ID, run.Output).Scan(&n); err != nil || n != 1 {
		t.Fatal("progress adoption is not one project memory", n, err)
	}
	after, err := f.Store.Snapshot(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]workspace.Item{}
	for _, it := range before.Projects {
		old[it.ID] = it
	}
	for _, it := range after.Projects {
		if it.Progress != old[it.ID].Progress || strings.Join(it.NextSteps, "|") != strings.Join(old[it.ID].NextSteps, "|") {
			t.Fatal("legacy progress/nextSteps were written")
		}
	}
	phase3Undo(t, h, f, action)
	var still int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_records r JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1 AND c.scope->>'project_id'=$2 AND c.value #>> '{}'=$3 AND r.state='active'`, f.Scope.OwnerID, p.ID, run.Output).Scan(&still); err != nil || still != 0 {
		t.Fatal("undo retained adopted active memory", still, err)
	}
}
