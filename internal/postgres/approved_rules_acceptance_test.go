package postgres

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// These expectations come from contracts §1.4/§2.1, not the execution algorithm.
func approvedRulesReceipt(t *testing.T, out workspace.DeskTurnResponse, index int, status string) workspace.DeskReceipt {
	t.Helper()
	if len(out.Turn.Receipts) <= index {
		t.Fatal("missing receipt", index, out.Turn.Receipts)
	}
	r := out.Turn.Receipts[index]
	if r.Status != status {
		t.Fatalf("receipt %d: want %s, got %+v", index, status, r)
	}
	if status == "skipped" && (r.Reason == "" || r.ActionID != nil || r.Undoable) {
		t.Fatal("skipped action lacks reason or owns undo", r)
	}
	if status == "done" && (r.ActionID == nil || !r.Undoable || r.ThingID == nil) {
		t.Fatal("successful action lacks target/log", r)
	}
	return r
}
func approvedRulesTask(t *testing.T, st workspace.State, id string) workspace.Item {
	t.Helper()
	for _, item := range st.Tasks {
		if item.ID == id {
			return item
		}
	}
	t.Fatal("missing task", id)
	return workspace.Item{}
}
func TestApprovedRules_MultipleObjectsAndProjectReferences(t *testing.T) {
	s, scope := testStore(t), owner()
	payload := `{"actions":[{"op":"create_project","name":"课程"},{"op":"create_task","title":"作业A","project":"N1"},{"op":"create_task","title":"作业B"},{"op":"add_steps","ref":"N2","steps":["只给A"]},{"op":"add_steps","ref":"N3","steps":["只给B"]},{"op":"update","ref":"N3","set":{"project":"N1"}},{"op":"create_idea","title":"新想法"},{"op":"update","ref":"N7","set":{"title":"已改想法"}}]}`
	stabilizationUndoModel(t, s, &payload)
	out := mustTurn(t, s, scope, turnRequest("建项目、两个任务、想法，各改各的"))
	if len(out.Turn.Receipts) != 8 || len(out.State.Tasks) != 2 || len(out.State.Projects) != 1 || len(out.State.Ideas) != 1 {
		t.Fatal(out.Turn.Receipts, out.State)
	}
	for i := range out.Turn.Receipts {
		approvedRulesReceipt(t, out, i, "done")
	}
	a, b := *out.Turn.Receipts[1].ThingID, *out.Turn.Receipts[2].ThingID
	if a == b || *out.Turn.Receipts[3].ThingID != a || *out.Turn.Receipts[4].ThingID != b {
		t.Fatal("new object aliases collided", out.Turn.Receipts)
	}
	for id, want := range map[string]string{a: "只给A", b: "只给B"} {
		task := approvedRulesTask(t, out.State, id)
		if len(task.Checklist) != 1 || task.Checklist[0].Text != want || task.ProjectID != out.State.Projects[0].ID {
			t.Fatal(task)
		}
	}
	if out.State.Ideas[0].Title != "已改想法" || *out.Turn.Receipts[7].ThingID != out.State.Ideas[0].ID {
		t.Fatal(out.State.Ideas)
	}
	// An unrelated new object's later action cannot prevent reverse undo of A.
	stabilizationUndoApply(t, s, scope, *out.Turn.Receipts[3].ActionID)
	st := stabilizationUndoApply(t, s, scope, *out.Turn.Receipts[1].ActionID)
	if len(st.Tasks) != 1 || approvedRulesTask(t, st, b).Checklist[0].Text != "只给B" {
		t.Fatal(st.Tasks)
	}
}
func TestApprovedRules_OriginalPositionsSurviveParseFailure(t *testing.T) {
	s, scope := testStore(t), owner()
	payload := `{"actions":[{"op":"create_task","title":42},{"op":"create_task","title":"位置二"},{"op":"add_steps","ref":"N1","steps":["不得执行"]},{"op":"add_steps","ref":"N2","steps":["正确步骤"]}]}`
	stabilizationUndoModel(t, s, &payload)
	out := mustTurn(t, s, scope, turnRequest("先错再建"))
	if len(out.Turn.Receipts) != 4 || len(out.State.Tasks) != 1 {
		t.Fatal(out.Turn.Receipts, out.State.Tasks)
	}
	for _, i := range []int{0, 2} {
		approvedRulesReceipt(t, out, i, "skipped")
	}
	for _, i := range []int{1, 3} {
		approvedRulesReceipt(t, out, i, "done")
	}
	if len(out.State.Tasks[0].Checklist) != 1 || out.State.Tasks[0].Checklist[0].Text != "正确步骤" {
		t.Fatal(out.State.Tasks)
	}
}
func TestApprovedRules_CreationFailureDoesNotBindOrRenumber(t *testing.T) {
	s, scope := testStore(t), owner()
	_, err := s.pool.Exec(context.Background(), `CREATE FUNCTION t4_reject_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.title='失败创建' THEN RAISE EXCEPTION 'T4 isolated rejection'; END IF; RETURN NEW; END $$; CREATE TRIGGER t4_reject_creation BEFORE INSERT ON work_items FOR EACH ROW EXECUTE FUNCTION t4_reject_creation()`)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"actions":[{"op":"create_task","title":"失败创建"},{"op":"create_task","title":"成功创建"},{"op":"add_steps","ref":"N1","steps":["不得执行"]},{"op":"add_steps","ref":"N2","steps":["正确步骤"]}]}`
	stabilizationUndoModel(t, s, &payload)
	out := mustTurn(t, s, scope, turnRequest("失败后继续"))
	if len(out.Turn.Receipts) != 4 || len(out.State.Tasks) != 1 {
		t.Fatal(out.Turn.Receipts, out.State.Tasks)
	}
	for _, i := range []int{0, 2} {
		approvedRulesReceipt(t, out, i, "skipped")
	}
	for _, i := range []int{1, 3} {
		approvedRulesReceipt(t, out, i, "done")
	}
	if out.State.Tasks[0].Title != "成功创建" || len(out.State.Tasks[0].Checklist) != 1 || out.State.Tasks[0].Checklist[0].Text != "正确步骤" {
		t.Fatal(out.State.Tasks)
	}
	var logs int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM action_log WHERE owner_id=$1 AND turn_id=$2", string(scope.OwnerID), out.Turn.ID).Scan(&logs); err != nil || logs != 2 {
		t.Fatal(logs, err)
	}
}
func TestApprovedRules_InvalidAliasesNeverFallBackToTHIS(t *testing.T) {
	cases := []struct {
		name, payload string
		skipped       []int
		tasks         int
	}{
		{"forward", `{"actions":[{"op":"add_steps","ref":"N2","steps":["禁止"]},{"op":"create_task","title":"新任务"}]}`, []int{0}, 2},
		{"out-of-range", `{"actions":[{"op":"add_steps","ref":"N99","steps":["禁止"]}]}`, []int{0}, 1},
		{"zero", `{"actions":[{"op":"add_steps","ref":"N0","steps":["禁止"]}]}`, []int{0}, 1},
		{"non-create", `{"actions":[{"op":"update","ref":"THIS","set":{"title":"合法改名"}},{"op":"add_steps","ref":"N1","steps":["禁止"]}]}`, []int{1}, 1},
		{"implicit-project", `{"actions":[{"op":"create_task","title":"新任务","project":"new:附带项目"},{"op":"create_task","title":"不得创建","project":"N1"}]}`, []int{1}, 2},
		{"wrong-kind-project", `{"actions":[{"op":"create_idea","title":"想法"},{"op":"update","ref":"THIS","set":{"project":"N1"}}]}`, []int{1}, 1},
		{"delegate-new", `{"actions":[{"op":"delegate","ref":"new","title":"附带任务","kind":"summary","prompt":"摘要"},{"op":"add_steps","ref":"N1","steps":["禁止"]}]}`, []int{1}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, scope := testStore(t), owner()
			st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "旧任务"})
			id := st.Tasks[0].ID
			payload := tc.payload
			stabilizationUndoModel(t, s, &payload)
			req := turnRequest("非法别名不可改旧任务")
			req.ThingID = &id
			out := mustTurn(t, s, scope, req)
			if len(out.State.Tasks) != tc.tasks {
				t.Fatal(out.State.Tasks, out.Turn.Receipts)
			}
			for _, i := range tc.skipped {
				approvedRulesReceipt(t, out, i, "skipped")
			}
			if tc.name == "forward" {
				approvedRulesReceipt(t, out, 1, "done")
			}
			if tc.name == "non-create" || tc.name == "implicit-project" || tc.name == "wrong-kind-project" || tc.name == "delegate-new" {
				approvedRulesReceipt(t, out, 0, "done")
			}
			if tc.name == "delegate-new" && len(out.State.Runs) != 1 {
				t.Fatal(out.State.Runs)
			}
			old := approvedRulesTask(t, out.State, id)
			if len(old.Checklist) != 0 || old.ProjectID != "" || (old.Title != "旧任务" && !(tc.name == "non-create" && old.Title == "合法改名")) {
				t.Fatal("old task changed via invalid N", old)
			}
		})
	}
}
func TestApprovedRules_ReplayNextTurnAndRAliasIsolation(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	payload := `{"actions":[{"op":"create_task","title":"会话任务"},{"op":"add_steps","ref":"N1","steps":["第一次"]}],"used":["N1"],"links":["N1"],"show":["N1"]}`
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); secretaryModelReply(w, payload) })
	req := turnRequest("创建并加步骤")
	first := mustTurn(t, s, scope, req)
	approvedRulesReceipt(t, first, 0, "done")
	approvedRulesReceipt(t, first, 1, "done")
	if len(first.State.Tasks) != 1 || len(first.State.Tasks[0].Checklist) != 1 || len(first.Turn.Cards) != 0 {
		t.Fatal(first)
	}
	id := first.State.Tasks[0].ID
	before := stabilizationUndoBusiness(t, s, scope, false)
	replay := mustTurn(t, s, scope, req)
	if calls.Load() != 1 || replay.Turn.ID != first.Turn.ID || *replay.Turn.Receipts[1].ActionID != *first.Turn.Receipts[1].ActionID {
		t.Fatal("replay reran model/actions", calls.Load(), replay)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
	payload = `{"actions":[{"op":"add_steps","ref":"N1","steps":["不得跨轮"]},{"op":"add_steps","ref":"R1","steps":["下一轮R"]}]}`
	next := turnRequest("下一轮操作")
	next.ConversationID = &first.ConversationID
	out := mustTurn(t, s, scope, next)
	approvedRulesReceipt(t, out, 0, "skipped")
	r := approvedRulesReceipt(t, out, 1, "done")
	if *r.ThingID != id || len(out.State.Tasks) != 1 || len(out.State.Tasks[0].Checklist) != 2 || out.State.Tasks[0].Checklist[1].Text != "下一轮R" {
		t.Fatal("N leaked or R target changed", out.Turn.Receipts, out.State.Tasks)
	}
}
func TestApprovedRules_TenActionLimitUsesOriginalArray(t *testing.T) {
	s, scope := testStore(t), owner()
	actions := []string{`{"op":"create_task","title":"前十内"}`}
	for i := 2; i <= 10; i++ {
		actions = append(actions, fmt.Sprintf(`{"op":"add_steps","ref":"N1","steps":["步骤%d"]}`, i))
	}
	actions = append(actions, `{"op":"create_task","title":"第十一禁止"}`, `{"op":"add_steps","ref":"N11","steps":["禁止"]}`)
	payload := `{"actions":[` + strings.Join(actions, ",") + `]}`
	stabilizationUndoModel(t, s, &payload)
	out := mustTurn(t, s, scope, turnRequest("十二动作只执行前十"))
	if len(out.State.Tasks) != 1 || out.State.Tasks[0].Title != "前十内" || len(out.State.Tasks[0].Checklist) != 9 {
		t.Fatal(out.State.Tasks, out.Turn.Receipts)
	}
	for i := 0; i < 10; i++ {
		approvedRulesReceipt(t, out, i, "done")
	}
	for i := 10; i < len(out.Turn.Receipts); i++ {
		approvedRulesReceipt(t, out, i, "skipped")
	}
}

func TestApprovedRules_ReturnToSameContentStillRequiresReverseOrder(t *testing.T) {
	for _, mode := range []string{"three-command-transactions", "one-desk-transaction"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			stabilizationUndoSnapshot(t, s, scope)
			before := stabilizationUndoBusiness(t, s, scope, true)
			var ids []string
			if mode == "three-command-transactions" {
				st, a := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "first"})
				_, b := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: st.Tasks[0].ID, Title: "second"})
				_, c := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: st.Tasks[0].ID, Title: "first"})
				ids = []string{a, b, c}
			} else {
				payload := `{"actions":[{"op":"create_task","title":"first"},{"op":"update","ref":"N1","set":{"title":"second"}},{"op":"update","ref":"N1","set":{"title":"first"}}]}`
				stabilizationUndoModel(t, s, &payload)
				out := mustTurn(t, s, scope, turnRequest("新建、改名、改回"))
				if len(out.Turn.Receipts) != 3 || len(out.State.Tasks) != 1 || out.State.Tasks[0].Title != "first" {
					t.Fatal(out.Turn.Receipts, out.State.Tasks)
				}
				for i := 0; i < 3; i++ {
					ids = append(ids, *approvedRulesReceipt(t, out, i, "done").ActionID)
				}
			}
			stabilizationUndoRefusal(t, s, scope, ids[0], "newer_action")
			stabilizationUndoRefusal(t, s, scope, ids[1], "newer_action")
			st := stabilizationUndoApply(t, s, scope, ids[2])
			if len(st.Tasks) != 1 || st.Tasks[0].Title != "second" {
				t.Fatal(st.Tasks)
			}
			stabilizationUndoRefusal(t, s, scope, ids[0], "newer_action")
			st = stabilizationUndoApply(t, s, scope, ids[1])
			if len(st.Tasks) != 1 || st.Tasks[0].Title != "first" {
				t.Fatal(st.Tasks)
			}
			stabilizationUndoApply(t, s, scope, ids[0])
			stabilizationUndoEqual(t, s, scope, before, true)
		})
	}
}

func TestApprovedRules_SameTurnR1AndN1RemainSeparate(t *testing.T) {
	s, scope := testStore(t), owner()
	payload := `{"actions":[{"op":"create_task","title":"上轮旧任务"}]}`
	stabilizationUndoModel(t, s, &payload)
	first := mustTurn(t, s, scope, turnRequest("建旧任务"))
	oldID := *approvedRulesReceipt(t, first, 0, "done").ThingID
	payload = `{"actions":[{"op":"create_task","title":"本轮新任务"},{"op":"add_steps","ref":"R1","steps":["只给旧任务"]},{"op":"add_steps","ref":"N1","steps":["只给新任务"]}]}`
	req := turnRequest("新旧对象分别操作")
	req.ConversationID = &first.ConversationID
	out := mustTurn(t, s, scope, req)
	if len(out.Turn.Receipts) != 3 || len(out.State.Tasks) != 2 {
		t.Fatal(out.Turn.Receipts, out.State.Tasks)
	}
	created := approvedRulesReceipt(t, out, 0, "done")
	oldChange, newChange := approvedRulesReceipt(t, out, 1, "done"), approvedRulesReceipt(t, out, 2, "done")
	if *created.ThingID == oldID || *oldChange.ThingID != oldID || *newChange.ThingID != *created.ThingID {
		t.Fatal("R/N aliases collided", out.Turn.Receipts)
	}
	for id, text := range map[string]string{oldID: "只给旧任务", *created.ThingID: "只给新任务"} {
		task := approvedRulesTask(t, out.State, id)
		if len(task.Checklist) != 1 || task.Checklist[0].Text != text {
			t.Fatal(task)
		}
	}
}
