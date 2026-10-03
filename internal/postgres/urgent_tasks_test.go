package postgres

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func urgentTask(t *testing.T, st workspace.State, title string) workspace.Item {
	t.Helper()
	for _, task := range st.Tasks {
		if task.Title == title {
			return task
		}
	}
	t.Fatalf("task %q missing", title)
	return workspace.Item{}
}

// H1, H2, H3, H6, H11: the secretary marks a to-do urgent only when told to,
// a later time or an unrelated edit leaves the mark alone, and undo restores it.
func TestSecretaryMarksAndClearsUrgentTasks(t *testing.T) {
	s, scope := testStore(t), owner()
	var output any
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })

	output = `{"reply":"记下了。","actions":[{"op":"create_task","title":"开始推广","urgent":true},{"op":"create_task","title":"交报告","urgent":null},{"op":"create_task","title":"订机票"}]}`
	created := mustTurn(t, s, scope, turnRequest("要尽快开始推广，不能拖了；另外交报告、订机票"))
	if !urgentTask(t, created.State, "开始推广").Urgent {
		t.Fatal("urgent create not marked")
	}
	if urgentTask(t, created.State, "交报告").Urgent || urgentTask(t, created.State, "订机票").Urgent {
		t.Fatal("null or absent urgent marked a task")
	}
	if text := created.Turn.Receipts[0].Text; !strings.HasSuffix(text, " · 尽快") {
		t.Fatalf("urgent receipt = %q", text)
	}
	if text := created.Turn.Receipts[1].Text; strings.Contains(text, "尽快") {
		t.Fatalf("ordinary receipt = %q", text)
	}

	// A time or a null leaves the mark as it was.
	due := testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Format("2006-01-02T15:04")
	output = `{"reply":"改好了。","actions":[{"op":"update","ref":"T1","set":{"due":"` + due + `","urgent":null}}]}`
	timed := mustTurn(t, s, scope, workspace.DeskTurnRequest{RequestID: turnRequest("").RequestID, AgentID: "model", Text: "推广那件事定在明天下午三点", ThingID: stringPointer(urgentTask(t, created.State, "开始推广").ID)})
	if got := urgentTask(t, timed.State, "开始推广"); !got.Urgent || got.Due == "" {
		t.Fatalf("time changed the mark: %+v", got)
	}

	// Marking, then undoing the change, returns to the earlier value.
	report := urgentTask(t, timed.State, "交报告")
	output = `{"reply":"好。","actions":[{"op":"update","ref":"THIS","set":{"urgent":true}}]}`
	marked := mustTurn(t, s, scope, workspace.DeskTurnRequest{RequestID: turnRequest("").RequestID, AgentID: "model", Text: "这个要赶紧", ThingID: stringPointer(report.ID)})
	if !urgentTask(t, marked.State, "交报告").Urgent || marked.Turn.Receipts[0].ActionID == nil {
		t.Fatalf("update did not mark: %+v", marked.Turn.Receipts)
	}
	undone := workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: *marked.Turn.Receipts[0].ActionID})
	if urgentTask(t, undone, "交报告").Urgent {
		t.Fatal("undo kept the mark")
	}

	output = `{"reply":"好，不急了。","actions":[{"op":"update","ref":"THIS","set":{"urgent":false}}]}`
	cleared := mustTurn(t, s, scope, workspace.DeskTurnRequest{RequestID: turnRequest("").RequestID, AgentID: "model", Text: "这件事不急了", ThingID: stringPointer(urgentTask(t, undone, "开始推广").ID)})
	if urgentTask(t, cleared.State, "开始推广").Urgent {
		t.Fatal("urgent false did not clear the mark")
	}

	// Undoing a creation removes the to-do with its mark.
	output = `{"reply":"记下了。","actions":[{"op":"create_task","title":"马上回电话","urgent":true}]}`
	again := mustTurn(t, s, scope, turnRequest("马上回电话"))
	gone := workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: *again.Turn.Receipts[0].ActionID})
	for _, task := range gone.Tasks {
		if task.Title == "马上回电话" {
			t.Fatal("undone creation remains")
		}
	}
}

// H7: the switch on the thing page is a plain task patch; ideas refuse it.
func TestUrgentIsATaskPatch(t *testing.T) {
	s, scope := testStore(t), owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "整理发票"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]bool{"urgent": true})})
	if !st.Tasks[0].Urgent {
		t.Fatal("patch did not mark")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]bool{"urgent": false})})
	if st.Tasks[0].Urgent {
		t.Fatal("patch did not clear")
	}
}

// The model is told when to set the mark, and the schema lets it.
func TestSecretaryIsToldAboutUrgent(t *testing.T) {
	if !strings.Contains(secretaryInstructions, "urgent：") || !strings.Contains(secretaryInstructions, "只说了一个具体时间不算着急") {
		t.Fatal("instructions do not explain urgent")
	}
	if strings.Count(string(secretaryOutputSchema), `"urgent"`) != 4 {
		t.Fatal("schema must declare and require urgent for create_task and update.set")
	}
}

// H10: migration 030 marks only open to-dos without a time whose title says so.
func TestUrgentMigrationMarksOnlyOpenUntimedTasks(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "尽快开始推广，不能再拖"})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "尽快交报告"})
	due := testsupport.DateFromToday(t, "Asia/Shanghai", 2, 9, 0).Format("2006-01-02T15:04:05Z07:00")
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: urgentTask(t, st, "尽快交报告").ID, Patch: asJSON(map[string]string{"due": due})})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "马上回电话"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: urgentTask(t, st, "马上回电话").ID, Status: "cancelled"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "整理发票"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "抓紧学游泳"})
	body, err := migrations.ReadFile("migrations/030_urgent_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	for title, want := range map[string]bool{"尽快开始推广，不能再拖": true, "尽快交报告": false, "马上回电话": false, "整理发票": false} {
		if got := urgentTask(t, after, title).Urgent; got != want {
			t.Errorf("%s urgent = %v", title, got)
		}
	}
	var ideas int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE owner_id=$1 AND kind='idea' AND document ? 'urgent'`, string(scope.OwnerID)).Scan(&ideas); err != nil || ideas != 0 {
		t.Fatalf("idea marked: %d %v", ideas, err)
	}
}
