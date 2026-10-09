package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// These contracts use synthetic decisions through the real gateway storage. An optional
// address reserves F14's assigned local port; CI uses an ephemeral listener.
type sameTurnModel struct {
	output   atomic.Value
	calls    atomic.Int32
	mu       sync.Mutex
	requests []string
}

func sameTurnFakeModel(t *testing.T, s *Store, output string) *sameTurnModel {
	t.Helper()
	fake := &sameTurnModel{}
	fake.output.Store(output)
	addr := os.Getenv("PCAS_F14_MODEL_ADDR")
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, string(body))
		fake.mu.Unlock()
		fake.calls.Add(1)
		secretaryModelReply(w, fake.output.Load().(string))
	})}}
	server.Start()
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "F14假秘书", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 2048, CostMode: "free"}}}})
	useSyntheticGateway(s)
	return fake
}

func sameTurnItem(t *testing.T, state workspace.State, id string) workspace.Item {
	t.Helper()
	for _, items := range [][]workspace.Item{state.Tasks, state.Ideas, state.Projects} {
		for _, item := range items {
			if item.ID == id {
				return item
			}
		}
	}
	t.Fatalf("missing item %s", id)
	return workspace.Item{}
}

func sameTurnReceipts(t *testing.T, s *Store, scope memory.Scope, out workspace.DeskTurnResponse, statuses ...string) {
	t.Helper()
	if len(out.Turn.Receipts) != len(statuses) {
		t.Fatalf("receipts: %+v", out.Turn.Receipts)
	}
	done := 0
	ids := map[string]bool{}
	for i, status := range statuses {
		r := out.Turn.Receipts[i]
		if r.Status != status {
			t.Fatalf("receipt %d: %+v, want %s", i+1, r, status)
		}
		if status == "skipped" {
			if r.Reason == "" || r.ActionID != nil || r.ThingID != nil || r.Undoable {
				t.Fatalf("unsafe skipped receipt %d: %+v", i+1, r)
			}
			continue
		}
		if r.ActionID == nil || r.ThingID == nil || !r.Undoable || ids[*r.ActionID] {
			t.Fatalf("missing independent undo record %d: %+v", i+1, r)
		}
		ids[*r.ActionID] = true
		done++
		var source, turn, summary string
		var changes []byte
		if err := s.pool.QueryRow(context.Background(), "SELECT source,turn_id::text,summary,changes FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), *r.ActionID).Scan(&source, &turn, &summary, &changes); err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			Table string `json:"table"`
			ID    string `json:"id"`
		}
		if err := json.Unmarshal(changes, &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Table == "work_items" && row.ID == *r.ThingID {
				found = true
			}
			if row.Table == "agent_runs" {
				for _, run := range out.State.Runs {
					if run.ID == row.ID && run.ThingID == *r.ThingID {
						found = true
					}
				}
			}
		}
		if source != "desk" || turn != out.Turn.ID || summary != r.Text || !found {
			t.Fatalf("wrong action log for receipt %d: %s %s %s %s", i+1, source, turn, summary, changes)
		}
	}
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM action_log WHERE owner_id=$1 AND turn_id=$2", string(scope.OwnerID), out.Turn.ID).Scan(&count); err != nil || count != done {
		t.Fatalf("logs=%d want=%d err=%v", count, done, err)
	}
}

func TestSameTurnCreateStepsReplayAndReverseUndo(t *testing.T) {
	s, scope := testStore(t), owner()
	old := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "旧事项"}).Tasks[0]
	fake := sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":"交作业"},{"op":"add_steps","ref":"N1","steps":["查资料","写提纲"]}],"show":["N1"],"used":["N1"],"links":["N1"]}`)
	req := turnRequest("建交作业任务，再给它加查资料、写提纲两个步骤")
	req.ThingID = &old.ID
	out := mustTurn(t, s, scope, req)
	sameTurnReceipts(t, s, scope, out, "done", "done")
	id := *out.Turn.Receipts[0].ThingID
	created := sameTurnItem(t, out.State, id)
	if id == old.ID || *out.Turn.Receipts[1].ThingID != id || len(created.Checklist) != 2 || created.Checklist[0].Text != "查资料" || created.Checklist[1].Text != "写提纲" || len(sameTurnItem(t, out.State, old.ID).Checklist) != 0 || len(out.Turn.Cards) != 0 {
		t.Fatal("wrong target/steps/cards", out)
	}
	if out.Turn.Receipts[0].Text != "已建：交作业" || out.Turn.Receipts[1].Text != "给「交作业」加了 2 步" {
		t.Fatal("receipt does not describe the new item", out.Turn.Receipts)
	}
	fake.mu.Lock()
	var request struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	err := json.Unmarshal([]byte(fake.requests[0]), &request)
	fake.mu.Unlock()
	if err != nil || len(request.Messages) == 0 || !strings.Contains(request.Messages[0].Content, "原 actions 数组里的序号") || !strings.Contains(request.Messages[0].Content, `"ref":"N1"`) {
		t.Fatal("actual model request omitted N protocol", err, request)
	}
	replayed := mustTurn(t, s, scope, req)
	if fake.calls.Load() != 1 || !bytes.Equal(asJSON(out.Turn), asJSON(replayed.Turn)) || len(sameTurnItem(t, replayed.State, id).Checklist) != 2 || len(replayed.State.Tasks) != 2 {
		t.Fatal("request replay repeated actions", replayed)
	}
	state, err := s.Undo(context.Background(), scope, *out.Turn.Receipts[1].ActionID)
	if err != nil || len(sameTurnItem(t, state, id).Checklist) != 0 {
		t.Fatal("steps undo failed", err, state)
	}
	state, err = s.Undo(context.Background(), scope, *out.Turn.Receipts[0].ActionID)
	if err != nil || len(state.Tasks) != 1 || state.Tasks[0].ID != old.ID {
		t.Fatal("create undo failed", err, state)
	}
	replayed = mustTurn(t, s, scope, req)
	if fake.calls.Load() != 1 || len(replayed.State.Tasks) != 1 || !replayed.Turn.Receipts[0].Undone || !replayed.Turn.Receipts[1].Undone {
		t.Fatal("replay recreated an undone task", replayed)
	}
}

func TestSameTurnMultipleCreationKindsAndProjectReferences(t *testing.T) {
	s, scope := testStore(t), owner()
	sameTurnFakeModel(t, s, `{"actions":[{"op":"create_project","name":"课程"},{"op":"create_task","title":"任务一","project":"N1"},{"op":"create_idea","title":"想法","project":"N1"},{"op":"update","ref":"N3","set":{"title":"新想法"}},{"op":"create_task","title":"任务二"},{"op":"update","ref":"N5","set":{"project":"N1"}},{"op":"add_steps","ref":"N2","steps":["第一步"]},{"op":"add_steps","ref":"N5","steps":["第二步"]},{"op":"update","ref":"N1","set":{"title":"新课程"}},{"op":"add_steps","ref":"N3","steps":["类型不合"]}]}`)
	out := mustTurn(t, s, scope, turnRequest("新项目下建任务和想法，分别修改"))
	sameTurnReceipts(t, s, scope, out, "done", "done", "done", "done", "done", "done", "done", "done", "done", "skipped")
	p := sameTurnItem(t, out.State, *out.Turn.Receipts[0].ThingID)
	a := sameTurnItem(t, out.State, *out.Turn.Receipts[1].ThingID)
	b := sameTurnItem(t, out.State, *out.Turn.Receipts[4].ThingID)
	idea := sameTurnItem(t, out.State, *out.Turn.Receipts[2].ThingID)
	if p.Title != "新课程" || a.ID == b.ID || a.ProjectID != p.ID || b.ProjectID != p.ID || idea.ProjectID != p.ID || idea.Title != "新想法" || len(a.Checklist) != 1 || a.Checklist[0].Text != "第一步" || len(b.Checklist) != 1 || b.Checklist[0].Text != "第二步" {
		t.Fatal("creation aliases mixed up", out.State)
	}
}

func TestSameTurnFailedParseAndRolledBackCreationKeepArrayPositions(t *testing.T) {
	s, scope := testStore(t), owner()
	old := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "旧事项"}).Tasks[0]
	// Fail the notes update after the new task's INSERT. This exercises a real
	// savepoint rollback, rather than only title validation before any writes.
	_, err := s.pool.Exec(context.Background(), `CREATE FUNCTION f14_fail_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.document->>'title'='创建事务失败' THEN RAISE EXCEPTION 'F14 injected write failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER f14_creation_failure BEFORE UPDATE ON work_items FOR EACH ROW EXECUTE FUNCTION f14_fail_creation()`)
	if err != nil {
		t.Fatal(err)
	}
	sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":42},{"op":"create_task","title":"创建事务失败","notes":"写入失败"},{"op":"add_steps","ref":"N1","steps":["不执行"]},{"op":"update","ref":"N2","set":{"title":"不执行"}},{"op":"create_task","title":"成功事项"},{"op":"add_steps","ref":"N5","steps":["正确位置"]},{"op":"add_steps","ref":"N1","steps":["不能重排"]},{"op":"add_steps","ref":"N2","steps":["不能残留"]}]}`)
	req := turnRequest("失败动作后继续新建并加步骤")
	req.ThingID = &old.ID
	out := mustTurn(t, s, scope, req)
	sameTurnReceipts(t, s, scope, out, "skipped", "skipped", "skipped", "skipped", "done", "done", "skipped", "skipped")
	if len(out.State.Tasks) != 2 || len(sameTurnItem(t, out.State, old.ID).Checklist) != 0 {
		t.Fatal("failed create leaked or old item changed", out.State.Tasks)
	}
	created := sameTurnItem(t, out.State, *out.Turn.Receipts[4].ThingID)
	if created.Title != "成功事项" || len(created.Checklist) != 1 || created.Checklist[0].Text != "正确位置" || *out.Turn.Receipts[5].ThingID != created.ID {
		t.Fatal("positions renumbered", out)
	}
}

func TestSameTurnInvalidNReferencesDoNotFallBack(t *testing.T) {
	for _, ref := range []string{"N0", "N-1", "N01", "n1", " N1", "N1 ", "N2", "N3", "N999"} {
		t.Run(ref, func(t *testing.T) {
			s, scope := testStore(t), owner()
			old := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "旧事项"}).Tasks[0]
			payload := string(asJSON(map[string]any{"actions": []any{map[string]string{"op": "create_task", "title": "新事项"}, map[string]any{"op": "update", "ref": ref, "set": map[string]string{"title": "不能改"}}, map[string]string{"op": "create_task", "title": "后面的事项"}}}))
			sameTurnFakeModel(t, s, payload)
			req := turnRequest("非法引用不能改旧事项")
			req.ThingID = &old.ID
			out := mustTurn(t, s, scope, req)
			sameTurnReceipts(t, s, scope, out, "done", "skipped", "done")
			if sameTurnItem(t, out.State, old.ID).Title != old.Title || sameTurnItem(t, out.State, *out.Turn.Receipts[0].ThingID).Title != "新事项" || len(out.State.Tasks) != 3 {
				t.Fatal("invalid ref changed another item", out.State.Tasks)
			}
		})
	}
}

func TestSameTurnOnlyExplicitCreateActionsBindAliases(t *testing.T) {
	s, scope := testStore(t), owner()
	sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":"附带项目任务","project":"new:附带项目"},{"op":"create_task","title":"类型不合","project":"N1"},{"op":"update","ref":"N1","set":{"title":"改名"}},{"op":"add_steps","ref":"N3","steps":["非创建"]},{"op":"delegate","ref":"new","title":"代理任务","kind":"plan","prompt":"列计划"},{"op":"add_steps","ref":"N5","steps":["不绑定代理"]},{"op":"create_task","title":"不绑定附带项目","project":"N5"},{"op":"create_task","title":"不猜标题","project":"附带项目"}]}`)
	out := mustTurn(t, s, scope, turnRequest("附带创建不建立N引用"))
	// Phase 3.5 C1 gives the incidental project its own undo receipt. N1
	// still binds the explicit task, not this extra project receipt.
	sameTurnReceipts(t, s, scope, out, "done", "done", "skipped", "done", "skipped", "done", "skipped", "skipped", "skipped")
	if out.Turn.Receipts[0].Op != "create_project" || out.Turn.Receipts[1].Op != "create_task" ||
		*out.Turn.Receipts[3].ThingID != *out.Turn.Receipts[1].ThingID {
		t.Fatal("incidental project receipt rebound N1", out.Turn.Receipts)
	}
	if len(out.State.Tasks) != 2 || len(out.State.Projects) != 1 || len(out.State.Runs) != 1 {
		t.Fatal("dependent action or incidental alias executed", out.State)
	}
}

func TestSameTurnDelegateTargetsCommittedCreation(t *testing.T) {
	s, scope := testStore(t), owner()
	old := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "旧事项"}).Tasks[0]
	sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":"交作业"},{"op":"delegate","ref":"N1","kind":"plan","prompt":"列交作业计划"},{"op":"add_steps","ref":"N2","steps":["非创建动作不绑定"]}]}`)
	req := turnRequest("建任务并交副手列计划")
	req.ThingID = &old.ID
	out := mustTurn(t, s, scope, req)
	sameTurnReceipts(t, s, scope, out, "done", "done", "skipped")
	createdID := *out.Turn.Receipts[0].ThingID
	if len(out.State.Runs) != 1 || out.State.Runs[0].ThingID != createdID || *out.Turn.Receipts[1].ThingID != createdID || len(sameTurnItem(t, out.State, old.ID).Checklist) != 0 {
		t.Fatal("delegation targeted an old object", out)
	}
	if _, err := s.Undo(context.Background(), scope, *out.Turn.Receipts[1].ActionID); err != nil {
		t.Fatal("queued delegation undo", err)
	}
	state, err := s.Undo(context.Background(), scope, *out.Turn.Receipts[0].ActionID)
	if err != nil || len(state.Runs) != 0 || len(state.Tasks) != 1 || state.Tasks[0].ID != old.ID {
		t.Fatal("reverse create/delegate undo", err, state)
	}
}

func TestSameTurnNExpiresWhileRecentAndThisKeepTheirTargets(t *testing.T) {
	s, scope := testStore(t), owner()
	old := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "页面事项"}).Tasks[0]
	fake := sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":"上一轮"}]}`)
	first := mustTurn(t, s, scope, turnRequest("建立已有对话对象"))
	previousID := *first.Turn.Receipts[0].ThingID
	fake.output.Store(`{"actions":[{"op":"create_task","title":"本轮"},{"op":"add_steps","ref":"N1","steps":["新对象步骤"]},{"op":"update","ref":"R1","set":{"title":"已有对话对象"}},{"op":"update","ref":"THIS","set":{"title":"页面对象"}}]}`)
	req := turnRequest("新建、加步骤、改已有事项和页面事项")
	req.ConversationID, req.ThingID = &first.ConversationID, &old.ID
	second := mustTurn(t, s, scope, req)
	sameTurnReceipts(t, s, scope, second, "done", "done", "done", "done")
	newID := *second.Turn.Receipts[0].ThingID
	if *second.Turn.Receipts[2].ThingID != previousID || *second.Turn.Receipts[3].ThingID != old.ID || sameTurnItem(t, second.State, previousID).Title != "已有对话对象" || sameTurnItem(t, second.State, old.ID).Title != "页面对象" || len(sameTurnItem(t, second.State, newID).Checklist) != 1 {
		t.Fatal("N changed R or THIS semantics", second)
	}
	fake.output.Store(`{"actions":[{"op":"add_steps","ref":"N1","steps":["跨轮不执行"]},{"op":"add_steps","ref":"R1","steps":["后轮R有效"]}]}`)
	req = turnRequest("下一轮使用R引用，N不可沿用")
	req.ConversationID = &first.ConversationID
	third := mustTurn(t, s, scope, req)
	sameTurnReceipts(t, s, scope, third, "skipped", "done")
	for _, task := range third.State.Tasks {
		for _, step := range task.Checklist {
			if step.Text == "跨轮不执行" {
				t.Fatal("N alias leaked between turns", third)
			}
		}
	}
	if fake.calls.Load() != 3 {
		t.Fatal("unexpected model calls", fake.calls.Load())
	}
}

func TestSameTurnOriginalTenActionLimit(t *testing.T) {
	s, scope := testStore(t), owner()
	actions := []any{map[string]string{"op": "create_task", "title": "上限任务"}}
	for i := 1; i < 12; i++ {
		actions = append(actions, map[string]any{"op": "add_steps", "ref": "N1", "steps": []string{"一步"}})
	}
	sameTurnFakeModel(t, s, string(asJSON(map[string]any{"actions": actions})))
	out := mustTurn(t, s, scope, turnRequest("原数组最多十个动作"))
	statuses := []string{"done", "done", "done", "done", "done", "done", "done", "done", "done", "done", "skipped"}
	sameTurnReceipts(t, s, scope, out, statuses...)
	if len(out.State.Tasks) != 1 || len(out.State.Tasks[0].Checklist) != 9 || out.Turn.Receipts[10].Reason != "一次太多了，只做了前 10 件" {
		t.Fatal("action limit changed", out)
	}
	// The existing reverse-undo contract applies to each of the ten actions.
	for i := 9; i >= 0; i-- {
		if _, err := s.Undo(context.Background(), scope, *out.Turn.Receipts[i].ActionID); err != nil {
			t.Fatalf("undo %d: %v", i+1, err)
		}
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Tasks) != 0 {
		t.Fatal("reverse undo left new task", err, state)
	}
}

func TestSameTurnRequestConflictDoesNotExecuteAgain(t *testing.T) {
	s, scope := testStore(t), owner()
	fake := sameTurnFakeModel(t, s, `{"actions":[{"op":"create_task","title":"交作业"},{"op":"add_steps","ref":"N1","steps":["查资料"]}]}`)
	req := turnRequest("建任务并加步骤")
	first := mustTurn(t, s, scope, req)
	req.Text = "同一个请求号的不同正文"
	_, err := s.DeskTurn(context.Background(), scope, req)
	if !errors.Is(err, memory.ErrConflict) || fake.calls.Load() != 1 {
		t.Fatal("conflicting request executed", err, fake.calls.Load())
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Tasks) != 1 || len(sameTurnItem(t, state, *first.Turn.Receipts[0].ThingID).Checklist) != 1 {
		t.Fatal("conflicting replay wrote state", err, state)
	}
}
