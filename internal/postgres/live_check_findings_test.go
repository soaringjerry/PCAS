package postgres

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Found by the live checklist on the real default channel: asked to break a
// task into steps, the model answered with a numbered list, which automatic
// adoption files as a document instead of subtasks. The request now states the
// line format adoption understands, for breakdowns only.
func TestBreakdownRequestStatesTheStepFormat(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "manual", Patch: asJSON(map[string]bool{"enabled": true})})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "周末出行准备"})
	task := st.Tasks[0]
	briefs := map[string]string{}
	for _, kind := range []string{"breakdown", "plan"} {
		st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: kind, Prompt: "拆成三步"})
		for _, run := range st.Runs {
			if run.Kind == kind {
				briefs[kind] = run.Brief
			}
		}
	}
	if !strings.Contains(briefs["breakdown"], "- [ ] ") || !strings.Contains(briefs["breakdown"], "不要编号") {
		t.Fatal("breakdown request does not state the step format", briefs["breakdown"])
	}
	if strings.Contains(briefs["plan"], "- [ ] ") {
		t.Fatal("the step format leaked into a non-breakdown request")
	}
	// What the instruction asks for is what adoption turns into subtasks; the
	// numbered list a real model produced without it is not.
	run := workspace.Run{Kind: "breakdown"}
	if got := adoptionFor(task, run, "- [ ] 确认出行信息\n- [ ] 整理准备清单\n- [ ] 出发前检查"); got != "subtasks" {
		t.Fatal(got)
	}
	if got := adoptionFor(task, run, "1. **确认出行信息**：…\n2. **整理准备清单**：…"); got != "doc" {
		t.Fatal("numbered lists are still filed as a document; the format instruction is what prevents them", got)
	}
}

// Deleting the original of an exchange is not the same as changing a memory
// the answer relied on. The conversation says which of the two happened.
func TestConversationSaysWhenTheOriginalWasDeleted(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"今天晴。","used":[],"show":[],"links":[],"actions":[]}`)
	})
	first := turnRequest("今天天气怎么样")
	out := mustTurn(t, s, scope, first)
	second := turnRequest("明天呢")
	second.ConversationID = &out.ConversationID
	mustTurn(t, s, scope, second)

	var source memory.Ref
	source.Kind = memory.SourceKind
	if err := s.pool.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk' AND lower(s.external_id)=$2`, string(scope.OwnerID), strings.ToLower(first.RequestID)).Scan(&source.ID, &source.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{source}}); err != nil {
		t.Fatal(err)
	}
	turns, err := s.DeskTurns(ctx, scope, out.ConversationID)
	if err != nil || len(turns.Turns) != 2 {
		t.Fatal(err, len(turns.Turns))
	}
	if deleted := turns.Turns[0]; deleted.Reply != "（内容已删除）" || deleted.Text != "" || len(deleted.Cards) != 0 {
		t.Fatalf("deleted original: reply=%q text=%q", deleted.Reply, deleted.Text)
	}
	if kept := turns.Turns[1]; kept.Reply != "今天晴。" || kept.Text != "明天呢" {
		t.Fatalf("the other exchange changed: reply=%q text=%q", kept.Reply, kept.Text)
	}
}
