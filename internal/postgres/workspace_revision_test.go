package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func revisionCommandClient(t *testing.T, s *Store, scope memory.Scope) func(workspace.Command, int, string) workspace.State {
	t.Helper()
	token := strings.Repeat("r", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	return func(command workspace.Command, status int, code string) workspace.State {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/workspace/commands", strings.NewReader(string(asJSON(command))))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s: HTTP %d, want %d: %s", command.Type, w.Code, status, w.Body.String())
		}
		var state workspace.State
		if code != "" {
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != code {
				t.Fatalf("%s: want %s: %s (%v)", command.Type, code, w.Body.String(), err)
			}
		} else if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
}

func bumpBackgroundRevision(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	// Simulate an unrelated worker commit without changing the action's rows.
	result, err := s.pool.Exec(context.Background(), "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("background revision: %v, rows=%d", err, result.RowsAffected())
	}
}

func TestWorkspaceStaleRevisionCommandsAndReplay(t *testing.T) {
	s := testStore(t)
	secretaryModel(t, s, func(http.ResponseWriter, *http.Request) { t.Error("queuing a run must not call the model") })
	for _, commandType := range []string{"setTaskStatus", "setNotes", "undoAction", "requestRun", "delegateTask"} {
		t.Run(commandType, func(t *testing.T) {
			scope := owner()
			send := revisionCommandClient(t, s, scope)
			created := workspace.Command{Type: "addTask", ID: string(memory.NewID()), Title: "回邮件", RequestID: string(memory.NewID())}
			before := send(created, http.StatusOK, "")
			command := workspace.Command{Type: commandType, ID: created.ID, RequestID: string(memory.NewID()), ExpectedRevision: before.Revision}
			switch commandType {
			case "setTaskStatus":
				command.Status = "done"
			case "setNotes":
				command.Text = "补充说明"
			case "undoAction":
				command.ID = created.RequestID
			case "requestRun":
				command.ID, command.ThingID = "", created.ID
				command.AgentID, command.Kind, command.Prompt = "manual", "plan", "帮我规划"
			case "delegateTask":
				command.ID, command.Title = string(memory.NewID()), "写方案"
				command.AgentID, command.Kind, command.Prompt = "model", "plan", "帮我规划"
			}
			bumpBackgroundRevision(t, s, scope)
			after := send(command, http.StatusOK, "")
			if after.Revision != before.Revision+2 {
				t.Fatalf("revision=%d, want %d", after.Revision, before.Revision+2)
			}
			switch commandType {
			case "setTaskStatus":
				if len(after.Tasks) != 1 || after.Tasks[0].Status != "done" {
					t.Fatal("status not saved", after.Tasks)
				}
			case "setNotes":
				if len(after.Tasks) != 1 || after.Tasks[0].Notes != command.Text {
					t.Fatal("notes not saved", after.Tasks)
				}
			case "undoAction":
				if len(after.Tasks) != 0 {
					t.Fatal("created task not undone", after.Tasks)
				}
			case "requestRun", "delegateTask":
				wantTasks := 1
				if commandType == "delegateTask" {
					wantTasks = 2
				}
				if len(after.Tasks) != wantTasks || len(after.Runs) != 1 {
					t.Fatal("run not queued exactly once", after.Tasks, after.Runs)
				}
			}
			bumpBackgroundRevision(t, s, scope)
			replayed := send(command, http.StatusOK, "")
			if replayed.Revision != after.Revision+1 || !reflect.DeepEqual(replayed.Tasks, after.Tasks) || !reflect.DeepEqual(replayed.Runs, after.Runs) {
				t.Fatal("replay executed again or returned an old snapshot")
			}
			// expectedRevision remains part of the request hash, even when it
			// no longer gates this command's first execution.
			changedRevision := command
			changedRevision.ExpectedRevision = replayed.Revision
			send(changedRevision, http.StatusConflict, "version_conflict")
			command.Text += "不同的请求体"
			send(command, http.StatusConflict, "version_conflict")
		})
	}
}

func TestWorkspaceStaleRevisionStillRejectsToggleAndBulk(t *testing.T) {
	s := testStore(t)
	scope := owner()
	send := revisionCommandClient(t, s, scope)
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "回邮件"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "addCheck", ID: state.Tasks[0].ID, Text: "写正文"})
	bumpBackgroundRevision(t, s, scope)
	for _, commandType := range []string{"toggleCheck", "toggleTrigger", "toggleContextMemory", "bulkStatus", "bulkDefer", "bulkMove", "bulkAccept", "bulkIgnore"} {
		t.Run(commandType, func(t *testing.T) {
			send := revisionCommandClient(t, s, scope)
			command := workspace.Command{Type: commandType, ID: state.Tasks[0].ID, ItemID: state.Tasks[0].Checklist[0].ID, IDs: []string{state.Tasks[0].ID}, Status: "done", Days: 1, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
			send(command, http.StatusConflict, "version_conflict")
		})
	}
	current, err := s.Snapshot(context.Background(), scope)
	if err != nil || current.Revision != state.Revision+1 || !reflect.DeepEqual(current.Tasks, state.Tasks) {
		t.Fatal("rejected command changed the workspace", err)
	}
	command := workspace.Command{Type: "toggleCheck", ID: state.Tasks[0].ID, ItemID: state.Tasks[0].Checklist[0].ID, RequestID: string(memory.NewID()), ExpectedRevision: current.Revision}
	after := send(command, http.StatusOK, "")
	if !after.Tasks[0].Checklist[0].Done {
		t.Fatal("fresh toggle did not check the step")
	}
	bumpBackgroundRevision(t, s, scope)
	replayed := send(command, http.StatusOK, "")
	if replayed.Revision != after.Revision+1 || !reflect.DeepEqual(replayed.Tasks, after.Tasks) {
		t.Fatal("toggle replay must deduplicate before checking revision")
	}
}

func TestWorkspaceStaleUndoStillChecksAfterHash(t *testing.T) {
	s := testStore(t)
	scope := owner()
	send := revisionCommandClient(t, s, scope)
	created := workspace.Command{Type: "addTask", Title: "回邮件", RequestID: string(memory.NewID())}
	state := send(created, http.StatusOK, "")
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: state.Tasks[0].ID, Text: "保留后续修改"})
	bumpBackgroundRevision(t, s, scope)
	send(workspace.Command{Type: "undoAction", ID: created.RequestID, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}, http.StatusConflict, "newer_action")
}
