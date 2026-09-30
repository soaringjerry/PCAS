package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUndoCommands(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	id := string(memory.NewID())
	request := string(memory.NewID())
	initial, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: id, Title: "回邮件", RequestID: request, ExpectedRevision: initial.Revision})
	if err != nil {
		t.Fatal(err)
	}
	doneRequest := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "setTaskStatus", ID: id, Status: "done", RequestID: doneRequest, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := st.Tasks[0].Version
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: doneRequest})
	if st.Tasks[0].Status != "todo" || st.Tasks[0].Version <= beforeVersion {
		t.Fatal("undo rolled back version or missed status", st.Tasks)
	}
	if _, err = s.Undo(ctx, scope, doneRequest); !errors.Is(err, workspace.ErrAlreadyUndone) {
		t.Fatal(err)
	}
	if _, err = s.Undo(ctx, scope, request); !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatal(err)
	}
	newRequest := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "addIdea", Title: "工具", RequestID: newRequest, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, newRequest)
	if err != nil || len(st.Ideas) != 0 {
		t.Fatal(err, st.Ideas)
	}
	if _, err = s.Undo(ctx, owner(), doneRequest); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("cross-owner undo", err)
	}
}
func TestUndoDocumentsAndFirstBefore(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "文档"})
	thing := st.Tasks[0].ID
	docID := string(memory.NewID())
	workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: docID, ThingID: thing, Title: "旧", Body: "原文", By: "user"}})
	request := string(memory.NewID())
	st, _ = s.Snapshot(ctx, scope)
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "deleteDoc", ID: docID, RequestID: request, ExpectedRevision: st.Revision})
	if err != nil || len(st.Docs) != 0 {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, request)
	if err != nil || len(st.Docs) != 1 || st.Docs[0].Body != "原文" {
		t.Fatal(err, st.Docs)
	}
	action := string(memory.NewID())
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		logCtx := withActionLog(ctx, action, "desk", "", "两次更新")
		if err := beginActionLogTx(logCtx, tx); err != nil {
			return err
		}
		if err := s.commandTx(logCtx, tx, scope, workspace.Command{Type: "renameThing", ID: thing, Title: "第一次"}); err != nil {
			return err
		}
		if err := s.commandTx(logCtx, tx, scope, workspace.Command{Type: "renameThing", ID: thing, Title: "第二次"}); err != nil {
			return err
		}
		return flushActionLog(logCtx, tx, scope)
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, action)
	if err != nil || st.Tasks[0].Title != "文档" {
		t.Fatal(err, st.Tasks)
	}
}
func TestUndoNewProjectDoesNotDetachLaterWork(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st, _ := s.Snapshot(ctx, scope)
	request := string(memory.NewID())
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "addProject", Name: "项目", RequestID: request, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "后来归入", ProjectID: st.Projects[0].ID})
	if _, err = s.Undo(ctx, scope, request); !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatal(err)
	}
}

func TestUndoQueuedDelegationAndStartedWork(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("undo must not call model") }))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "副手", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, started := range []bool{false, true} {
		id := string(memory.NewID())
		action := string(memory.NewID())
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			logged := withActionLog(ctx, action, "desk", "", "写方案")
			if err := beginActionLogTx(logged, tx); err != nil {
				return err
			}
			if err := s.commandTx(logged, tx, scope, workspace.Command{Type: "delegateTask", ID: id, Title: "写方案", Prompt: "写方案", AgentID: "model"}); err != nil {
				return err
			}
			return flushActionLog(logged, tx, scope)
		})
		if err != nil {
			t.Fatal(err)
		}
		if started {
			if _, err = s.pool.Exec(ctx, "UPDATE agent_runs SET status='running' WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), id); err != nil {
				t.Fatal(err)
			}
		}
		st, err := s.Undo(ctx, scope, action)
		if started {
			if !errors.Is(err, workspace.ErrWorkStarted) {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || len(st.Tasks) != 0 || len(st.Runs) != 0 || st.BudgetUsage != 0 {
			t.Fatal(err, st.Tasks, st.Runs, st.BudgetUsage)
		}
		var orphan int
		if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM run_dependencies WHERE owner_id=$1", string(scope.OwnerID)).Scan(&orphan); err != nil || orphan != 0 {
			t.Fatal(err, orphan)
		}
	}
}

func TestDueReminderTracksCommandDue(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "回邮件"})
	id := st.Tasks[0].ID
	loc, _ := time.LoadLocation("Asia/Shanghai")
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		item.Due = "2026-10-02T07:00:00Z"
		applyDueReminder(&item, "-30m", loc)
		return saveItem(ctx, tx, scope, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]string{"due": "2026-10-05T02:00:00Z"})})
	if len(st.Tasks[0].Triggers) != 1 || st.Tasks[0].Triggers[0].NextAt != "2026-10-05T01:30:00Z" {
		t.Fatal(st.Tasks[0].Triggers)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]string{"due": ""})})
	if len(st.Tasks[0].Triggers) != 0 {
		t.Fatal(st.Tasks[0].Triggers)
	}
}
func TestDueReminderOffsets(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for _, tc := range []struct{ offset, next string }{{"-2h", "2026-10-02T05:00:00Z"}, {"at", "2026-10-02T07:00:00Z"}, {"09:00", "2026-10-02T01:00:00Z"}} {
		item := workspace.Item{Title: "邮件", Due: "2026-10-02T07:00:00Z", Triggers: []workspace.Trigger{{ID: "other"}}}
		applyDueReminder(&item, tc.offset, loc)
		if len(item.Triggers) != 2 || item.Triggers[1].NextAt != tc.next || item.Triggers[1].Offset != tc.offset {
			t.Fatal(item.Triggers)
		}
		applyDueReminder(&item, "", loc)
		if item.Triggers[1].Offset != tc.offset {
			t.Fatal("lost offset")
		}
		applyDueReminder(&item, "none", loc)
		if len(item.Triggers) != 1 || item.Triggers[0].ID != "other" {
			t.Fatal(item.Triggers)
		}
	}
}
