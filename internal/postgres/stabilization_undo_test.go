package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The oracle reads stored business data, never action_log snapshots or hashes.
// Only the five bookkeeping keys expressly listed in contracts §1.2 are removed.
func stabilizationUndoBusiness(t *testing.T, s *Store, scope memory.Scope, normalize bool) string {
	t.Helper()
	out := map[string][]json.RawMessage{}
	for _, table := range []string{"work_items", "work_documents", "agent_runs", "training_samples", "workspace_notices"} {
		expression := "to_jsonb(r)"
		if normalize {
			// SQL version/updated_at mirror recordVersion/updatedAt; no other column is ignored.
			expression = "(to_jsonb(r)-'version'-'updated_at'-'document') || jsonb_build_object('document',document-ARRAY['recordVersion','updatedAt','history','evolution','sources'])"
			if table == "workspace_notices" {
				expression = "to_jsonb(r)"
			}
		}
		rows, err := s.pool.Query(context.Background(), "SELECT ("+expression+")::text FROM "+table+" r WHERE owner_id=$1 ORDER BY id", string(scope.OwnerID))
		if err != nil {
			t.Fatal(err)
		}
		out[table] = []json.RawMessage{}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[table] = append(out[table], json.RawMessage(raw))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func stabilizationUndoSnapshot(t *testing.T, s *Store, scope memory.Scope) workspace.State {
	t.Helper()
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
func stabilizationUndoCommand(t *testing.T, s *Store, scope memory.Scope, c workspace.Command) (workspace.State, string) {
	t.Helper()
	if c.RequestID == "" {
		c.RequestID = string(memory.NewID())
	}
	c.ExpectedRevision = stabilizationUndoSnapshot(t, s, scope).Revision
	st, err := s.Execute(context.Background(), scope, c)
	if err != nil {
		t.Fatalf("%s: %v", c.Type, err)
	}
	return st, c.RequestID
}
func stabilizationUndoApply(t *testing.T, s *Store, scope memory.Scope, id string) workspace.State {
	t.Helper()
	st, err := s.Undo(context.Background(), scope, id)
	if err != nil {
		t.Fatalf("undo %s: %v", id, err)
	}
	return st
}
func stabilizationUndoEqual(t *testing.T, s *Store, scope memory.Scope, want string, normalize bool) {
	t.Helper()
	if got := stabilizationUndoBusiness(t, s, scope, normalize); got != want {
		t.Fatalf("business state differs\nwant %s\ngot  %s", want, got)
	}
}
func stabilizationUndoHTTP(t *testing.T, s *Store, scope memory.Scope, c workspace.Command) (int, string) {
	t.Helper()
	token := strings.Repeat("u", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	if c.RequestID == "" {
		c.RequestID = string(memory.NewID())
	}
	c.ExpectedRevision = stabilizationUndoSnapshot(t, s, scope).Revision
	body, _ := json.Marshal(c)
	r := httptest.NewRequest("POST", "/v1/workspace/commands", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}
func stabilizationUndoModel(t *testing.T, s *Store, payload *string) {
	t.Helper()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, *payload) })
}
func stabilizationUndoTurn(t *testing.T, s *Store, scope memory.Scope, text string) workspace.DeskTurnResponse {
	t.Helper()
	return mustTurn(t, s, scope, turnRequest(text))
}
func stabilizationUndoAction(t *testing.T, out workspace.DeskTurnResponse, index int) string {
	t.Helper()
	if len(out.Turn.Receipts) <= index || out.Turn.Receipts[index].ActionID == nil || !out.Turn.Receipts[index].Undoable {
		t.Fatalf("no action receipt: %+v", out.Turn.Receipts)
	}
	return *out.Turn.Receipts[index].ActionID
}
func stabilizationUndoScheduled(t *testing.T, s *Store, scope memory.Scope) (workspace.DeskTurnResponse, time.Time) {
	t.Helper()
	due := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	payload := fmt.Sprintf(`{"actions":[{"op":"create_task","title":"T1 reminder","due":%q,"remind":"at"}]}`, due.Format(time.RFC3339))
	stabilizationUndoModel(t, s, &payload)
	out := stabilizationUndoTurn(t, s, scope, "创建待撤销提醒")
	if len(out.State.Tasks) != 1 || len(out.State.Tasks[0].Triggers) != 1 || out.State.Tasks[0].Triggers[0].NextAt != due.Format(time.RFC3339) {
		t.Fatalf("missing reminder fixture: %+v", out.State.Tasks)
	}
	return out, due
}
func TestStabilizationUndoU1_CreateDeletesReminderAndNotice(t *testing.T) {
	s := testStore(t)
	scope := owner()
	stabilizationUndoSnapshot(t, s, scope)
	before := stabilizationUndoBusiness(t, s, scope, true)
	out, due := stabilizationUndoScheduled(t, s, scope)
	id := out.State.Tasks[0].ID
	_, err := s.pool.Exec(context.Background(), "INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,'due-reminder',$3,'fixture notice')", string(scope.OwnerID), id, due)
	if err != nil {
		t.Fatal(err)
	}
	st := stabilizationUndoApply(t, s, scope, stabilizationUndoAction(t, out, 0))
	if len(st.Tasks) != 0 || len(st.Notices) != 0 {
		t.Fatal(st.Tasks, st.Notices)
	}
	stabilizationUndoEqual(t, s, scope, before, true)
}
func TestStabilizationUndoU2_ReverseCreationAndEdit(t *testing.T) {
	s := testStore(t)
	scope := owner()
	stabilizationUndoSnapshot(t, s, scope)
	before := stabilizationUndoBusiness(t, s, scope, true)
	st, created := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "original", Text: "retain notes"})
	id := st.Tasks[0].ID
	middle := stabilizationUndoBusiness(t, s, scope, true)
	st, edited := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "edited"})
	version := st.Tasks[0].Version
	st = stabilizationUndoApply(t, s, scope, edited)
	if st.Tasks[0].Version <= version {
		t.Fatal("version rolled back")
	}
	stabilizationUndoEqual(t, s, scope, middle, true)
	stabilizationUndoApply(t, s, scope, created)
	stabilizationUndoEqual(t, s, scope, before, true)
}
func stabilizationUndoPendingRefusal(t *testing.T, s *Store, scope memory.Scope, action string) {
	t.Helper()
	before := stabilizationUndoBusiness(t, s, scope, false)
	revision := stabilizationUndoSnapshot(t, s, scope).Revision
	status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: action})
	if status != 409 {
		t.Fatalf("expected refusal, got %d %s", status, body)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
	if stabilizationUndoSnapshot(t, s, scope).Revision != revision {
		t.Fatal("refused undo changed revision")
	}
	var undone bool
	if err := s.pool.QueryRow(context.Background(), "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&undone); err != nil || undone {
		t.Fatal("refused action marked undone", err)
	}
	t.Logf("safety verified; observed HTTP %d %s", status, body)
}
func TestStabilizationUndoU3_SkipLaterEditPendingCode(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st, created := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "first"})
	stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: st.Tasks[0].ID, Title: "later"})
	stabilizationUndoPendingRefusal(t, s, scope, created)
	t.Skip("pending U3: newer_action versus changed_since requires user ruling; safety passed, sequence not accepted")
}
func TestStabilizationUndoU4_UserAndBackendEditPendingCode(t *testing.T) {
	for _, mode := range []string{"user-command", "unlogged-backend"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			st, created := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "first"})
			id := st.Tasks[0].ID
			if mode == "user-command" {
				stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "manual change"})
			} else {
				_, err := s.pool.Exec(context.Background(), "UPDATE work_items SET title='background change',document=jsonb_set(document,'{title}','\"background change\"') WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
				if err != nil {
					t.Fatal(err)
				}
			}
			stabilizationUndoPendingRefusal(t, s, scope, created)
			if mode == "unlogged-backend" {
				status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: created})
				if status != 409 || !strings.Contains(body, `"error":"changed_since"`) {
					t.Fatal(status, body)
				}
				return
			}
			t.Skip("pending U4: logged user edit overlaps newer_action/changed_since; safety passed")
		})
	}
}
func TestStabilizationUndoU5_TwoEditsPendingCode(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "base"})
	id := st.Tasks[0].ID
	_, a := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "A"})
	stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "B"})
	stabilizationUndoPendingRefusal(t, s, scope, a)
	t.Skip("pending U5: newer_action versus changed_since requires user ruling; safety passed")
}
func TestStabilizationUndoU6_CompleteRestoreFutureReminder(t *testing.T) {
	s := testStore(t)
	scope := owner()
	out, due := stabilizationUndoScheduled(t, s, scope)
	before := stabilizationUndoBusiness(t, s, scope, true)
	_, action := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: out.State.Tasks[0].ID, Status: "done"})
	st := stabilizationUndoApply(t, s, scope, action)
	if st.Tasks[0].Status != "todo" || !st.Tasks[0].Triggers[0].Active {
		t.Fatal(st.Tasks)
	}
	stabilizationUndoEqual(t, s, scope, before, true)
	if err := s.CheckReminders(context.Background(), due); err != nil {
		t.Fatal(err)
	}
	if len(stabilizationUndoSnapshot(t, s, scope).Notices) != 1 {
		t.Fatal("restored reminder did not fire")
	}
}
func TestStabilizationUndoU7_DeleteAfterNotificationDelivered(t *testing.T) {
	s := testStore(t)
	scope := owner()
	stabilizationUndoSnapshot(t, s, scope)
	before := stabilizationUndoBusiness(t, s, scope, true)
	out, due := stabilizationUndoScheduled(t, s, scope)
	if err := s.CheckReminders(context.Background(), due); err != nil {
		t.Fatal(err)
	}
	channel := &fakeNotifyChannel{name: "T1-local"}
	if err := s.DispatchNotices(context.Background(), due, []notify.Channel{channel}); err != nil {
		t.Fatal(err)
	}
	if len(channel.calls) != 1 {
		t.Fatal("notice not delivered", channel.calls)
	}
	stabilizationUndoApply(t, s, scope, stabilizationUndoAction(t, out, 0))
	stabilizationUndoEqual(t, s, scope, before, true)
	if len(channel.calls) != 1 {
		t.Fatal("undo attempted outbound retraction")
	}
}
func stabilizationUndoCompletedRun(t *testing.T, s *Store, scope memory.Scope, thing, output string) (workspace.State, string, string) {
	t.Helper()
	stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "manual", Kind: "breakdown", Prompt: "T1 fixture"})
	st := stabilizationUndoSnapshot(t, s, scope)
	runID := st.Runs[len(st.Runs)-1].ID
	// Locate the newest queued fixture without relying on snapshot ordering.
	for _, run := range st.Runs {
		if run.ThingID == thing && run.Status == "waiting" && run.Output == "" {
			runID = run.ID
		}
	}
	st, _ = stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: runID, Output: output})
	var action string
	for _, run := range st.Runs {
		if run.ID == runID && run.Adopted != nil {
			action = run.Adopted.ActionID
		}
	}
	if action == "" {
		t.Fatal("result did not auto-adopt", st.Runs)
	}
	return st, runID, action
}
func TestStabilizationUndoU8_AutoAdoptUndoReadoptUndo(t *testing.T) {
	for _, path := range []string{"manual-result", "model-worker"} {
		t.Run(path, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			output := "- [ ] alpha\n- [ ] beta"
			if path == "model-worker" {
				autoAdoptModel(t, s, output, nil)
			}
			st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "base"})
			item := st.Tasks[0]
			var runID, auto string
			if path == "manual-result" {
				st, runID, auto = stabilizationUndoCompletedRun(t, s, scope, item.ID, output)
			} else {
				st, _ = stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: item.ID, AgentID: "auto-model", Kind: "breakdown", Prompt: "T1 local worker"})
				runID = st.Runs[0].ID
				if err := s.runAgentOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				st = stabilizationUndoSnapshot(t, s, scope)
				if len(st.Runs) != 1 || st.Runs[0].Adopted == nil {
					t.Fatal("worker did not auto-adopt", st.Runs)
				}
				auto = st.Runs[0].Adopted.ActionID
			}
			if len(st.Tasks[0].Checklist) != 2 || len(st.Samples) != 1 || !st.Runs[0].Adopted.Auto {
				t.Fatal(st.Tasks, st.Samples, st.Runs)
			}
			cost := st.Runs[0].Cost
			st = stabilizationUndoApply(t, s, scope, auto)
			if st.Runs[0].Status != "done" || st.Runs[0].Adopted != nil || st.Runs[0].Output != output || st.Runs[0].Cost != cost || len(st.Tasks[0].Checklist) != len(item.Checklist) || len(st.Samples) != 0 {
				t.Fatal(st.Runs, st.Tasks, st.Samples)
			}
			before := stabilizationUndoBusiness(t, s, scope, true)
			st, manual := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: runID, As: "subtasks", Text: "alpha\nbeta"})
			if len(st.Samples) != 1 || st.Runs[0].Adopted == nil || st.Runs[0].Adopted.Auto {
				t.Fatal(st.Samples, st.Runs)
			}
			stabilizationUndoApply(t, s, scope, manual)
			stabilizationUndoEqual(t, s, scope, before, true)
		})
	}
}
func TestStabilizationUndoU9_CheckChangeBlocksAdoptionUndo(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "base"})
	id := st.Tasks[0].ID
	st, _, action := stabilizationUndoCompletedRun(t, s, scope, id, "- [ ] alpha")
	stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "toggleCheck", ID: id, ItemID: st.Tasks[0].Checklist[0].ID})
	before := stabilizationUndoBusiness(t, s, scope, false)
	status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: action})
	if status != 409 || !strings.Contains(body, `"error":"changed_since"`) {
		t.Fatal(status, body)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
}
func TestStabilizationUndoU10_ExistingItemSameTurnPartialCoverage(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "before turn"})
	id := st.Tasks[0].ID
	before := stabilizationUndoBusiness(t, s, scope, true)
	payload := `{"actions":[{"op":"update","ref":"THIS","set":{"title":"same turn update"}},{"op":"add_steps","ref":"THIS","steps":["first","second"]}]}`
	stabilizationUndoModel(t, s, &payload)
	req := turnRequest("改名再加步骤")
	req.ThingID = &id
	out := mustTurn(t, s, scope, req)
	if len(out.Turn.Receipts) != 2 || len(out.State.Tasks[0].Checklist) != 2 {
		t.Fatal("existing THIS fixture failed", out.Turn.Receipts)
	}
	updated, steps := stabilizationUndoAction(t, out, 0), stabilizationUndoAction(t, out, 1)
	stabilizationUndoPendingRefusal(t, s, scope, updated)
	st = stabilizationUndoApply(t, s, scope, steps)
	if st.Tasks[0].Title != "same turn update" || len(st.Tasks[0].Checklist) != 0 {
		t.Fatal(st.Tasks)
	}
	stabilizationUndoApply(t, s, scope, updated)
	stabilizationUndoEqual(t, s, scope, before, true)
	t.Skip("pending U10: existing THIS update/add_steps reverse sequence passed; create/add_steps has no contracted current-turn reference; skip-order code depends on U3 ruling")
}
func TestStabilizationUndoU11_RequestReplayAndNewRequest(t *testing.T) {
	s := testStore(t)
	scope := owner()
	_, action := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "one"})
	cmd := workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: stabilizationUndoSnapshot(t, s, scope).Revision}
	first, err := s.Execute(context.Background(), scope, cmd)
	if err != nil {
		t.Fatal(err)
	}
	before := stabilizationUndoBusiness(t, s, scope, false)
	replay, err := s.Execute(context.Background(), scope, cmd)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("replay differs", err, first.Revision, replay.Revision)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
	status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: action})
	if status != 409 || !strings.Contains(body, `"error":"already_undone"`) {
		t.Fatal(status, body)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
	status, body = stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: string(memory.NewID())})
	if status != 404 || !strings.Contains(body, `"error":"not_found"`) {
		t.Fatal(status, body)
	}
	if _, err := s.Undo(context.Background(), owner(), action); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("owner boundary", err)
	}
}
func TestStabilizationUndoU12_DeletedSourceExpiresSnapshot(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "capture", Text: "T1 private source"})
	candidate := st.Candidates[0]
	st, _ = stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: "fact", Text: "T1 private source"})
	memoryID := st.Memories[0].ID
	st, _ = stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "affected item"})
	id := st.Tasks[0].ID
	source, _ := json.Marshal(candidate.Source)
	if _, err := s.pool.Exec(context.Background(), "UPDATE work_items SET document=jsonb_set(document,'{sources}',jsonb_build_array($3::jsonb)) WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, string(source)); err != nil {
		t.Fatal(err)
	}
	_, action := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: id, Text: "snapshot content"})
	stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: memoryID, IncludeSources: true})
	var cleared bool
	if err := s.pool.QueryRow(context.Background(), "SELECT changes='[]'::jsonb AND expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&cleared); err != nil || !cleared {
		t.Fatal("snapshot retained", err)
	}
	before := stabilizationUndoBusiness(t, s, scope, false)
	status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: action})
	if status != 409 {
		t.Fatal("expired snapshot accepted", status, body)
	}
	stabilizationUndoEqual(t, s, scope, before, false)
	stabilizationUndoFinding(t, "T1-U12")
	if !strings.Contains(body, `"error":"expired"`) {
		t.Fatalf("U12 requires HTTP 409 expired; got %d %s", status, body)
	}
}
func TestStabilizationUndoU13_ThirtyDayExpiry(t *testing.T) {
	for _, flush := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup=%v", flush), func(t *testing.T) {
			stabilizationUndoFinding(t, "T1-U13")
			s := testStore(t)
			scope := owner()
			_, action := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "old"})
			_, err := s.pool.Exec(context.Background(), "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action)
			if err != nil {
				t.Fatal(err)
			}
			if flush {
				stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "retention sweep"})
			}
			before := stabilizationUndoBusiness(t, s, scope, false)
			status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: action})
			if status != 409 || !strings.Contains(body, `"error":"expired"`) {
				t.Fatalf("U13 requires HTTP 409 expired; got %d %s", status, body)
			}
			stabilizationUndoEqual(t, s, scope, before, false)
		})
	}
}
func TestStabilizationUndoU14_QueuedAndStartedDelegation(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprintf("started=%v", started), func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			payload := `{"actions":[{"op":"delegate","ref":"new","title":"delegated","kind":"plan","prompt":"make a plan"}]}`
			stabilizationUndoModel(t, s, &payload)
			s.models.Config.Providers[0].CostMode = "token"
			s.models.Config.Providers[0].InputPerMillion = 2
			s.models.Config.Providers[0].OutputPerMillion = 3
			s.models.Config.Providers[0].MaxOutput = 200
			stabilizationUndoSnapshot(t, s, scope)
			before := stabilizationUndoBusiness(t, s, scope, true)
			out := stabilizationUndoTurn(t, s, scope, "新建并派副手")
			var queued bool
			if len(out.State.Runs) == 1 {
				if err := s.pool.QueryRow(context.Background(), "SELECT status='queued' AND reserved_cost>0 FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.State.Runs[0].ID).Scan(&queued); err != nil {
					t.Fatal(err)
				}
			}
			var reserved float64
			if len(out.State.Runs) == 1 {
				if err := s.pool.QueryRow(context.Background(), "SELECT reserved_cost FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.State.Runs[0].ID).Scan(&reserved); err != nil {
					t.Fatal(err)
				}
			}
			if len(out.State.Runs) != 1 || !queued || out.State.BudgetUsage <= 0 {
				t.Fatal("no reserved queued run", out.State.Runs, out.State.BudgetUsage)
			}
			if started {
				_, err := s.pool.Exec(context.Background(), "UPDATE agent_runs SET status='running',document=jsonb_set(document,'{status}','\"running\"') WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.State.Runs[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				current := stabilizationUndoBusiness(t, s, scope, false)
				status, body := stabilizationUndoHTTP(t, s, scope, workspace.Command{Type: "undoAction", ID: stabilizationUndoAction(t, out, 0)})
				if status != 409 || !strings.Contains(body, `"error":"work_started"`) {
					t.Fatal(status, body)
				}
				stabilizationUndoEqual(t, s, scope, current, false)
				return
			}
			st := stabilizationUndoApply(t, s, scope, stabilizationUndoAction(t, out, 0))
			if len(st.Tasks) != 0 || len(st.Runs) != 0 || math.Abs(st.BudgetUsage-(out.State.BudgetUsage-reserved)) > 0.000001 {
				t.Fatal("reservation not released", st.Tasks, st.Runs, st.BudgetUsage, out.State.BudgetUsage, reserved)
			}
			stabilizationUndoEqual(t, s, scope, before, true)
			var dependencies int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM run_dependencies WHERE owner_id=$1", string(scope.OwnerID)).Scan(&dependencies); err != nil || dependencies != 0 {
				t.Fatal("orphaned run dependencies", err, dependencies)
			}
		})
	}
}
func TestStabilizationUndoU16_FiftySeededReverseSequences(t *testing.T) {
	s := testStore(t)
	for seed := int64(1); seed <= 50; seed++ {
		t.Run(fmt.Sprintf("seed_%02d", seed), func(t *testing.T) {
			scope := owner()
			rng := rand.New(rand.NewSource(seed))
			st, _ := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "baseline", Text: "original notes"})
			base := st.Tasks[0].ID
			doc := string(memory.NewID())
			stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: doc, ThingID: base, Title: "baseline doc", Body: "original body", By: "user"}})
			_, runID, auto := stabilizationUndoCompletedRun(t, s, scope, base, "- [ ] baseline generated step")
			stabilizationUndoApply(t, s, scope, auto)
			initial := stabilizationUndoBusiness(t, s, scope, true)
			actions := []string{}
			states := []string{}
			sequence := []string{}
			adopted := false
			targets := []string{base}
			// Six mandatory families in each seed, then fourteen random legal actions.
			families := rng.Perm(6)
			for len(families) < 20 {
				families = append(families, rng.Intn(6))
			}
			for step, family := range families {
				st = stabilizationUndoSnapshot(t, s, scope)
				targetIndex := rng.Intn(len(targets))
				target := workspace.Item{}
				for _, item := range st.Tasks {
					if item.ID == targets[targetIndex] {
						target = item
					}
				}
				if target.ID == "" {
					t.Fatal("model target missing")
				}
				c := workspace.Command{}
				switch family {
				case 0:
					c = workspace.Command{Type: "addTask", ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", seed*100+int64(step)+1), Title: fmt.Sprintf("seed %d task %d", seed, step), Text: "new notes"}
				case 1:
					c = workspace.Command{Type: "renameThing", ID: target.ID, Title: fmt.Sprintf("seed %d rename %d", seed, step)}
				case 2:
					status := "done"
					if target.Status == "done" {
						status = "todo"
					}
					c = workspace.Command{Type: "setTaskStatus", ID: target.ID, Status: status}
				case 3:
					c = workspace.Command{Type: "addCheck", ID: target.ID, Text: fmt.Sprintf("step %d", step)}
				case 4:
					if !adopted {
						c = workspace.Command{Type: "adoptRun", ID: runID, As: "subtasks", Text: "generated step"}
						adopted = true
					} else {
						c = workspace.Command{Type: "setNotes", ID: target.ID, Text: fmt.Sprintf("notes %d", step)}
					}
				case 5:
					c = workspace.Command{Type: "updateDoc", ID: doc, Patch: asJSON(map[string]string{"title": fmt.Sprintf("doc %d", step), "body": fmt.Sprintf("body %d seed %d", step, seed)})}
				}
				states = append(states, stabilizationUndoBusiness(t, s, scope, true))
				sequence = append(sequence, fmt.Sprintf("%02d:%s target-index=%d title=%s text=%s status=%s patch=%s", step, c.Type, targetIndex, c.Title, c.Text, c.Status, c.Patch))
				var action string
				st, action = stabilizationUndoCommand(t, s, scope, c)
				actions = append(actions, action)
				if c.Type == "addTask" {
					targets = append(targets, c.ID)
				}
			}
			t.Logf("seed=%d sequence=%s", seed, strings.Join(sequence, " | "))
			for i := len(actions) - 1; i >= 0; i-- {
				_, err := s.Undo(context.Background(), scope, actions[i])
				if err != nil {
					t.Fatalf("seed=%d undo-step=%d action=%s err=%v sequence=%s", seed, i, actions[i], err, strings.Join(sequence, " | "))
				}
				stabilizationUndoEqual(t, s, scope, states[i], true)
			}
			stabilizationUndoEqual(t, s, scope, initial, true)
		})
	}
}
func TestStabilizationUndoLegacyActionLogCompatibility(t *testing.T) {
	for _, mode := range []string{"unchanged", "bookkeeping-change", "business-change"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			st, action := stabilizationUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "legacy"})
			id := st.Tasks[0].ID
			// Migration 016 documents the historical full-document PostgreSQL hash.
			_, err := s.pool.Exec(context.Background(), `UPDATE action_log SET changes=jsonb_set(changes,'{0,afterHash}',to_jsonb(encode(sha256(convert_to((SELECT document::text FROM work_items WHERE owner_id=$1 AND id=$3),'UTF8')),'hex'))) WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), action, id)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "bookkeeping-change" {
				_, err = s.pool.Exec(context.Background(), "UPDATE work_items SET version=version+1,document=jsonb_set(document,'{recordVersion}',to_jsonb(version+1)) WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
			}
			if mode == "business-change" {
				_, err = s.pool.Exec(context.Background(), "UPDATE work_items SET title='real edit',document=jsonb_set(document,'{title}','\"real edit\"') WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := stabilizationUndoBusiness(t, s, scope, false)
			st, err = s.Undo(context.Background(), scope, action)
			if mode == "unchanged" {
				if err != nil || len(st.Tasks) != 0 {
					t.Fatal("untouched legacy action failed", err, st.Tasks)
				}
				t.Log("unchanged historical full-document hash remains undoable")
				return
			}
			if !errors.Is(err, workspace.ErrChangedSince) {
				t.Fatal("changed legacy action must be refused", err)
			}
			stabilizationUndoEqual(t, s, scope, before, false)
			t.Logf("observed legacy %s refusal: %v", mode, err)
		})
	}
}

// Reserved for confirmed findings after an actual failing run, with strict
// assertions retained below each call. Set this env to reproduce all findings.
func stabilizationUndoFinding(t *testing.T, id string) {
	t.Helper()
	if os.Getenv("PCAS_STABILIZATION_UNDO_RECHECK") != "1" {
		t.Skip("finding " + id + ": see docs/evaluations/2026-10-01-stabilization-undo.md; PCAS_STABILIZATION_UNDO_RECHECK=1 reruns strict assertions")
	}
}
