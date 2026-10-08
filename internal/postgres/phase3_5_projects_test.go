package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase35Turn(t *testing.T, s *Store, scope memory.Scope, text string) workspace.DeskTurnResponse {
	t.Helper()
	h := phase35HTTP(t, s, scope)
	code, raw, err := h.call(context.Background(), "POST", "/v1/desk/turn", workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "model", Text: text})
	if err != nil || code != 200 {
		t.Fatalf("real secretary HTTP status=%d body=%s err=%v", code, raw, err)
	}
	var out workspace.DeskTurnResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPhase35C1C3SecretaryProjectCreationAndUndo(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	var instructions string
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		for _, m := range input.Messages {
			if m.Role == "system" {
				instructions = m.Content
			}
		}
		secretaryModelReply(w, `{"reply":"建了项目「虚构青岚展览」，步骤归入其中。","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[{"op":"create_task","title":"虚构征集陶瓷标本","project":"new:虚构青岚展览"},{"op":"create_task","title":"虚构排出展览方案","project":"new:虚构青岚展览"}],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`)
	})
	out := phase35Turn(t, s, scope, "虚构目标：月底办青岚展览，要先征集陶瓷标本再排方案。")
	if len(out.State.Projects) != 1 || len(out.State.Tasks) != 2 {
		t.Fatalf("project/steps=%d/%d", len(out.State.Projects), len(out.State.Tasks))
	}
	project := out.State.Projects[0].ID
	for _, it := range out.State.Tasks {
		if it.ProjectID != project {
			t.Fatal("step not grouped", it)
		}
	}
	if strings.Contains(instructions, "只有用户明确新建项目") {
		t.Fatal("C1 old explicit-project-only gate remains in prompt")
	}
	for _, criterion := range []string{"目标", "截止", "好几步", "好几天", "已有项目"} {
		if !strings.Contains(instructions, criterion) {
			t.Fatalf("model lacks project criterion %q", criterion)
		}
	}
	var wire struct{ Creation *struct{ By string } }
	_ = json.Unmarshal(asJSON(out.State.Projects[0]), &wire)
	if wire.Creation == nil || wire.Creation.By != "secretary" {
		t.Fatal("secretary project lost provenance")
	}
	action := ""
	for _, r := range out.Turn.Receipts {
		if strings.Contains(r.Text, "建了项目") && r.ActionID != nil {
			action = *r.ActionID
		}
	}
	if action == "" {
		t.Fatal("project creation missing own undoable receipt")
	}
	memoriesBefore := len(out.State.Memories)
	after, err := s.Undo(ctx, scope, action)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Projects) != 0 || len(after.Tasks) != 2 || len(after.Memories) != memoriesBefore {
		t.Fatalf("project undo deleted steps/memories projects=%d tasks=%d", len(after.Projects), len(after.Tasks))
	}
	for _, it := range after.Tasks {
		if it.ProjectID != "" {
			t.Fatal("project undo did not detach step", it)
		}
	}
}
func TestPhase35C1SingleStepAndC3ModelExistingProjectAlias(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	s, _ := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	before, _ := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "虚构青岚展览"})
	// Distinct user phrase is deliberately resolved by the model to P1. Code
	// must use that alias; it must not compare the phrase to the project name.
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"归入已有展览。","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[{"op":"create_task","title":"虚构校对陶瓷展标签","project":"P1"}],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`)
	})
	out := phase35Turn(t, s, scope, "青湾陶瓷节那件事，我要校对一张标签。")
	if len(out.State.Projects) != 1 || len(out.State.Tasks) != 1 || out.State.Tasks[0].ProjectID != before.Projects[0].ID {
		t.Fatal("model-existing-project alias created extra project")
	}
}
func TestPhase35C4AutomaticProjectEntersHandoverSchedule(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"建了项目「虚构青岚多日目标」。","actions":[{"op":"create_task","title":"虚构第一步","project":"new:虚构青岚多日目标"}],"used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`)
	})
	out := phase35Turn(t, s, scope, "虚构未来几天完成青岚陶瓷资料，先做第一步。")
	if len(out.State.Projects) != 1 {
		t.Fatal("automatic project missing")
	}
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.project_handover:%' AND state='queued'`, scope.OwnerID).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("automatic project not scheduled: jobs=%d err=%v", n, err)
	}
}
