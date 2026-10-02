package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryHistoryOneEntryPerAction(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	output := secretaryOutput{Actions: []secretaryAction{{
		Op: "create_task", Title: "给张三回邮件", Due: stringPointer(testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Format("2006-01-02T15:04")),
		Project: stringPointer("new:A"), Notes: stringPointer("核对附件"),
		OwedTo: stringPointer("张三"), WaitingFor: stringPointer("确认方案"),
	}}}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })
	req := turnRequest("周五下午三点给张三回邮件，提前半小时提醒")
	created := mustTurn(t, s, scope, req)
	if len(created.State.Tasks) != 1 || len(created.Turn.Receipts) != 1 {
		t.Fatal(created)
	}
	task := created.State.Tasks[0]
	if len(task.History) != 1 || task.History[0].By != "secretary" || !strings.HasPrefix(task.History[0].Summary, "新建：") || !strings.Contains(task.History[0].Summary, "15:00 给张三回邮件 · A 项目 · 14:30 提醒") {
		t.Fatalf("creation must have one meaningful secretary revision: %+v", task.History)
	}
	if task.Notes != "核对附件" || task.OwedTo == nil || task.OwedTo.Who != "张三" || task.WaitingFor != "确认方案" || len(task.Triggers) != 1 || task.Triggers[0].NextAt != testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Add(-30*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatal("creation lost fields or reminder", task)
	}
	if replay := mustTurn(t, s, scope, req); !reflect.DeepEqual(replay.State.Tasks[0].History, task.History) {
		t.Fatal("replay appended history", replay.State.Tasks[0].History)
	}
	var sourceBody string
	if err := s.pool.QueryRow(ctx, `SELECT v.body FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id)
		WHERE s.owner_id=$1 AND s.connector='actions' AND s.external_id=$2 ORDER BY v.version DESC LIMIT 1`, string(scope.OwnerID), task.ID).Scan(&sourceBody); err != nil {
		t.Fatal(err)
	}
	var published workspace.Item
	if err := json.Unmarshal([]byte(sourceBody), &published); err != nil || !reflect.DeepEqual(published.History, task.History) || !reflect.DeepEqual(published.Triggers, task.Triggers) {
		t.Fatal("memory source must describe the final history and reminder", published, err)
	}
	assertLog := func(receipt workspace.DeskReceipt, before *workspace.Item, count int) {
		t.Helper()
		if receipt.ActionID == nil || !receipt.Undoable {
			t.Fatal("missing undoable action", receipt)
		}
		var raw []byte
		var source, summary string
		if err := s.pool.QueryRow(ctx, "SELECT source,summary,changes FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), *receipt.ActionID).Scan(&source, &summary, &raw); err != nil {
			t.Fatal(err)
		}
		var changes []actionChange
		if err := json.Unmarshal(raw, &changes); err != nil || source != "desk" || summary != receipt.Text || len(changes) != count {
			t.Fatal("action log changed", source, summary, string(raw), err)
		}
		for _, change := range changes {
			if change.Table != "work_items" || change.AfterHash == nil {
				t.Fatal(change)
			}
			if change.ID == task.ID {
				var hash string
				if err := s.pool.QueryRow(ctx, "SELECT action_document_hash(document) FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), task.ID).Scan(&hash); err != nil || hash != *change.AfterHash {
					t.Fatal("action log must hash the final content", hash, err)
				}
				if before == nil {
					if string(change.Before) != "null" {
						t.Fatal("creation has a before snapshot", change)
					}
				} else {
					var got workspace.Item
					if err := json.Unmarshal(change.Before, &got); err != nil || !reflect.DeepEqual(got, *before) {
						t.Fatal("first before snapshot lost", got, err)
					}
				}
				return
			}
		}
		t.Fatal("task absent from action log")
	}
	assertLog(created.Turn.Receipts[0], nil, 2) // task and implicit project
	output.Actions = []secretaryAction{{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"due": asJSON(testsupport.DateFromToday(t, "Asia/Shanghai", 4, 10, 0).Format("2006-01-02T15:04"))}}}
	req = turnRequest("改到周一十点")
	req.ThingID = &task.ID
	updated := mustTurn(t, s, scope, req)
	changed := updated.State.Tasks[0]
	if len(changed.History) != 2 || changed.History[0] != task.History[0] || changed.History[1].By != "secretary" || !strings.HasPrefix(changed.History[1].Summary, "更新：") {
		t.Fatal("reschedule must append exactly one revision", changed.History)
	}
	if changed.Due != testsupport.DateFromToday(t, "Asia/Shanghai", 4, 10, 0).UTC().Format(time.RFC3339) || changed.Triggers[0].NextAt != testsupport.DateFromToday(t, "Asia/Shanghai", 4, 10, 0).Add(-30*time.Minute).UTC().Format(time.RFC3339) || !strings.Contains(changed.History[1].Summary, "09:30 提醒") {
		t.Fatal("reschedule did not preserve reminder", changed)
	}
	assertLog(updated.Turn.Receipts[0], &task, 1)
	if replay := mustTurn(t, s, scope, req); !reflect.DeepEqual(replay.State.Tasks[0].History, changed.History) {
		t.Fatal("update replay appended history", replay.State.Tasks[0].History)
	}
	restored, err := s.Undo(ctx, scope, *updated.Turn.Receipts[0].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	got := restored.Tasks[0]
	if got.Due != task.Due || !reflect.DeepEqual(got.Triggers, task.Triggers) || got.Version <= changed.Version || len(got.History) != 2 || got.History[1].By != "user" || !strings.HasPrefix(got.History[1].Summary, "撤销：") {
		t.Fatal("undo semantics changed", got)
	}
	removed, err := s.Undo(ctx, scope, *created.Turn.Receipts[0].ActionID)
	if err != nil || len(removed.Tasks) != 0 || len(removed.Projects) != 0 {
		t.Fatal("undo creation after reschedule undo must remove task and implicit project", err, removed.Tasks, removed.Projects)
	}
}

func TestSecretaryHistoryBatchesOnlyWithinOneAction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create secretaryAction
		update secretaryAction
		kind   string
	}{
		{"plain task and steps", secretaryAction{Op: "create_task", Title: "任务"}, secretaryAction{Op: "add_steps", Ref: "THIS", Steps: []string{"核对", "准备", "发送"}}, "task"},
		{"reminder only", secretaryAction{Op: "create_task", Title: "提醒", Due: stringPointer(testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Format("2006-01-02T15:04"))}, secretaryAction{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"remind": asJSON("-1h")}}, "task"},
		{"idea with condition", secretaryAction{Op: "create_idea", Title: "想法", Condition: stringPointer("下周再看"), ConditionDue: stringPointer(testsupport.DateFromToday(t, "Asia/Shanghai", 4, 23, 59).Format("2006-01-02"))}, secretaryAction{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"title": asJSON("新想法"), "notesAppend": asJSON("补充")}}, "idea"},
		{"project", secretaryAction{Op: "create_project", Name: "项目"}, secretaryAction{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"title": asJSON("新项目"), "notesAppend": asJSON("目标")}}, "project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			output := secretaryOutput{Actions: []secretaryAction{tc.create}}
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })
			find := func(st workspace.State) workspace.Item {
				t.Helper()
				items := map[string][]workspace.Item{"task": st.Tasks, "idea": st.Ideas, "project": st.Projects}[tc.kind]
				if len(items) != 1 {
					t.Fatal("expected one item", items)
				}
				return items[0]
			}
			created := mustTurn(t, s, scope, turnRequest("新建"))
			item := find(created.State)
			if len(item.History) != 1 || item.History[0].By != "secretary" || (tc.kind == "idea" && len(item.Evolution) != 1) {
				t.Fatal("duplicate creation history", item)
			}
			// Two independent actions in one turn must remain two revisions.
			output.Actions = []secretaryAction{tc.update, {Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"notesAppend": asJSON("另一动作")}}}
			req := turnRequest("修改，再补一条说明")
			req.ThingID = &item.ID
			updated := mustTurn(t, s, scope, req)
			changed := find(updated.State)
			if len(changed.History) != 3 || changed.History[1].By != "secretary" || changed.History[2].By != "secretary" || changed.Version <= item.Version || (tc.kind == "idea" && len(changed.Evolution) != 3) {
				t.Fatal("history must be scoped to each action", changed)
			}
			if tc.name == "reminder only" && (changed.Triggers[0].Offset != "-1h" || changed.Triggers[0].NextAt == item.Triggers[0].NextAt) {
				t.Fatal("reminder-only edit was lost", changed)
			}
			// A separate creation can still be removed completely by undo.
			output.Actions = []secretaryAction{tc.create}
			another := mustTurn(t, s, scope, turnRequest("再新建"))
			if _, err := s.Undo(context.Background(), scope, *another.Turn.Receipts[0].ActionID); err != nil {
				t.Fatal("creation undo failed", err)
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil || find(st).ID != item.ID {
				t.Fatal("undo did not remove only the new item", st, err)
			}
		})
	}
}
