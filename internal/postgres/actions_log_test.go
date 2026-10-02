package postgres

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestUndoIgnoresOnlyBookkeeping(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	for _, field := range []string{"recordVersion", "updatedAt", "history", "evolution", "sources"} {
		t.Run(field, func(t *testing.T) {
			request, id := string(memory.NewID()), string(memory.NewID())
			st, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: id, Title: "簿记", RequestID: request})
			if err != nil {
				t.Fatal(err)
			}
			// Change each field independently without changing the task's content.
			// The original trigger fingerprint must still permit undo.
			value := asJSON([]any{map[string]string{"text": "新的簿记"}})
			if field == "recordVersion" {
				value = asJSON(st.Tasks[0].Version + 1)
			} else if field == "updatedAt" {
				value = asJSON("2026-10-01T00:00:00Z")
			}
			if _, err := s.pool.Exec(ctx, "UPDATE work_items SET document=jsonb_set(document,ARRAY[$3::text],$4::jsonb) WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, field, value); err != nil {
				t.Fatal(err)
			}
			if st, err := s.Undo(ctx, scope, request); err != nil || len(st.Tasks) != 0 {
				t.Fatal("bookkeeping blocked undo", err, st.Tasks)
			}
		})
	}
}

func TestUndoRejectsContentChanges(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	project := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "项目"}).Projects[0].ID
	for _, edit := range []workspace.Command{
		{Type: "renameThing", Title: "用户改了标题"},
		{Type: "setTaskStatus", Status: "cancelled"},
		{Type: "updateTask", Patch: asJSON(map[string]string{"due": "2026-10-02T08:00:00Z"})},
		{Type: "updateTask", Patch: asJSON(map[string]string{"scheduled": "2026-10-02T08:00:00Z"})},
		{Type: "addCheck", Text: "新步骤"},
		{Type: "setNotes", Text: "新的说明"},
		{Type: "moveThing", ProjectID: project},
	} {
		t.Run(edit.Type+string(edit.Patch), func(t *testing.T) {
			request, id := string(memory.NewID()), string(memory.NewID())
			if _, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: id, Title: "原来的事项", RequestID: request}); err != nil {
				t.Fatal(err)
			}
			edit.ID = id
			workspaceCommand(t, s, scope, edit)
			if _, err := s.Undo(ctx, scope, request); !errors.Is(err, workspace.ErrNewerAction) {
				t.Fatal("content edit must block undo", err)
			}
			var undone bool
			if err := s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), request).Scan(&undone); err != nil || undone {
				t.Fatal("failed undo changed action log", err, undone)
			}
		})
	}
}

func TestUndoLegacyFingerprint(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	for _, changed := range []bool{false, true} {
		request, id := string(memory.NewID()), string(memory.NewID())
		if _, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: id, Title: "迁移前的事项", RequestID: request}); err != nil {
			t.Fatal(err)
		}
		// Reproduce an action written by migration 016's trigger, which stored
		// only the full-document after hash and no after snapshot.
		if _, err := s.pool.Exec(ctx, `UPDATE action_log SET changes=jsonb_set(changes,'{0,afterHash}',
			(SELECT to_jsonb(encode(sha256(convert_to(document::text,'UTF8')),'hex'))
			 FROM work_items WHERE owner_id=$1 AND id=$3)) WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), request, id); err != nil {
			t.Fatal(err)
		}
		if changed {
			workspaceCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "后来修改"})
		}
		st, err := s.Undo(ctx, scope, request)
		if changed {
			if !errors.Is(err, workspace.ErrNewerAction) {
				t.Fatal("legacy fingerprint accepted content change", err)
			}
		} else if err != nil || len(st.Tasks) != 0 {
			t.Fatal("legacy fingerprint no longer accepted", err, st.Tasks)
		}
	}
}

func TestUndoAdoptionThenEarlierEdit(t *testing.T) {
	for _, kind := range []string{"breakdown", "summary", "draft"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			ctx := context.Background()
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "原来的标题"})
			id := st.Tasks[0].ID
			earlier := string(memory.NewID())
			st, err := s.Execute(ctx, scope, workspace.Command{Type: "renameThing", ID: id, Title: "新标题", RequestID: earlier})
			if err != nil {
				t.Fatal(err)
			}
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: kind, Prompt: "处理"})
			output := "一段成果"
			if kind == "breakdown" {
				output = "- [ ] 第一步\n- [ ] 第二步"
			}
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: st.Runs[0].ID, Output: output})
			if st.Runs[0].Adopted == nil || !st.Runs[0].Adopted.Auto {
				t.Fatal("fixture did not auto-adopt", st.Runs)
			}
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: st.Runs[0].Adopted.ActionID})
			if len(st.Samples) != 0 || len(st.Docs) != 0 || st.Runs[0].Adopted != nil || len(st.Tasks[0].Checklist) != 0 || st.Tasks[0].Notes != "" {
				t.Fatal("adoption undo left artifacts", st)
			}
			st, err = s.Undo(ctx, scope, earlier)
			if err != nil || st.Tasks[0].Title != "原来的标题" || st.Runs[0].Status != "done" || st.Runs[0].Output != output {
				t.Fatal("earlier edit undo failed after adoption undo", err, st.Tasks, st.Runs)
			}
		})
	}
}

func TestUndoRejectsReminderChange(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, secretaryOutput{Actions: []secretaryAction{{Op: "create_task", Title: "提醒", Due: stringPointer(testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Format("2006-01-02T15:04"))}}})
	})
	created := mustTurn(t, s, scope, turnRequest("新建提醒"))
	task := created.State.Tasks[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "toggleTrigger", ID: task.ID, TriggerID: task.Triggers[0].ID})
	if _, err := s.Undo(context.Background(), scope, *created.Turn.Receipts[0].ActionID); !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatal("reminder change must block undo", err)
	}
}
