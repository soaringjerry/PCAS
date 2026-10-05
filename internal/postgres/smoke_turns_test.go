package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func smokeCounts(t *testing.T, s *Store, scope memory.Scope) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"memory_records", "record_versions", "claims", "sources", "source_versions", "source_extractions", "desk_turns", "desk_turn_order", "work_items", "capture_candidates", "memory_jobs", "action_log", "agent_runs", "work_documents", "training_samples", "workspace_notices", "desk_smoke_changes", "desk_smoke_actions"} {
		var n int
		if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE owner_id=$1", string(scope.OwnerID)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

func TestSmokeThreeSentencesLeaveNothing(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	due := testsupport.DateFromToday(t, "UTC", 1, 15, 0).Format(time.RFC3339)
	later := testsupport.DateFromToday(t, "UTC", 3, 10, 0).Format(time.RFC3339)
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1, 2:
			secretaryModelReply(w, secretaryOutput{Reply: "5。", Remember: true})
		case 3:
			secretaryModelReply(w, secretaryOutput{Reply: "安排好了。", Remember: true, Actions: []secretaryAction{{Op: "create_task", Title: "虚构栀岑晾纸", Due: &due}}})
		case 4:
			secretaryModelReply(w, secretaryOutput{Reply: "改好了。", Remember: true, Actions: []secretaryAction{{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"due": asJSON(later)}}}})
		default:
			secretaryModelReply(w, secretaryOutput{Reply: "正常对话。"})
		}
	})
	// Nonempty ordinary memory, original, item and candidate must all survive.
	organizeTestMemory(t, s, scope, "虚构人物雾棘喜欢窄边纸。")
	workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "虚构人物雾棘的正常便签。"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构常规事项"})
	ordinary := mustTurn(t, s, scope, turnRequest("虚构常规问题：二加三是多少？"))
	if _, err := s.ScheduleOrganize(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	before := smokeCounts(t, s, scope)
	t.Logf("before=%v", before)
	token := "synthetic-owner-token-for-smoke-test"
	api := httpapi.New(memory.NewService(s), s, httpapi.NewOwnerToken(token, scope.OwnerID), s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(string(asJSON(body))))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	group := string(memory.NewID())
	question := turnRequest("虚构检查：二加三是多少？")
	question.SmokeID = group
	var first workspace.DeskTurnResponse
	if err := json.Unmarshal(call("POST", "/v1/desk/turn", question, 200), &first); err != nil {
		t.Fatal(err)
	}
	if first.Turn.Reply != "5。" || first.ConversationID != group || len(first.Turn.Receipts) != 0 {
		t.Fatal(first)
	}
	var replay workspace.DeskTurnResponse
	if err := json.Unmarshal(call("POST", "/v1/desk/turn", question, 200), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Turn.ID != first.Turn.ID || calls.Load() != 2 {
		t.Fatal("check retry was not idempotent")
	}
	arrange := turnRequest("安排虚构栀岑晾纸，时间是" + due)
	arrange.SmokeID = group
	var second workspace.DeskTurnResponse
	if err := json.Unmarshal(call("POST", "/v1/desk/turn", arrange, 200), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Turn.Receipts) != 1 || second.Turn.Receipts[0].Status != "done" || second.Turn.Receipts[0].ThingID == nil {
		t.Fatal(second)
	}
	itemID := *second.Turn.Receipts[0].ThingID
	modify := turnRequest("把刚才的虚构安排改到" + later)
	modify.SmokeID, modify.ThingID = group, &itemID
	var third workspace.DeskTurnResponse
	if err := json.Unmarshal(call("POST", "/v1/desk/turn", modify, 200), &third); err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, item := range third.State.Tasks {
		if item.ID == itemID {
			changed = item.Due == later && len(item.Triggers) == 1
		}
	}
	if !changed || calls.Load() != 4 || third.Turn.Reply != "改好了。" {
		t.Fatal("actions did not actually execute", third)
	}
	during := smokeCounts(t, s, scope)
	for _, table := range []string{"memory_records", "record_versions", "claims", "sources", "source_versions", "source_extractions", "capture_candidates", "memory_jobs"} {
		if during[table] != before[table] {
			t.Fatalf("check turn reached %s: %v -> %v", table, before, during)
		}
	}
	if during["desk_turns"] != before["desk_turns"]+3 || during["work_items"] != before["work_items"]+1 {
		t.Fatal("expected temporary originals and real task", during)
	}
	if n, err := s.BackfillExtractions(ctx, time.Now()); err != nil || n != 0 {
		t.Fatal("check originals reached backfill", n, err)
	}
	if n, err := s.ScheduleOrganize(ctx, time.Now()); err != nil || n != 0 {
		t.Fatal("check reached organizing", n, err)
	}
	call("DELETE", "/v1/desk/smoke/"+group, nil, 200)
	after := smokeCounts(t, s, scope)
	t.Logf("after=%v", after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("cleanup counts changed: before=%v after=%v", before, after)
	}
	kept, err := s.DeskTurns(ctx, scope, ordinary.ConversationID)
	if err != nil || len(kept.Turns) != 1 || kept.Turns[0].ID != ordinary.Turn.ID {
		t.Fatal("ordinary history affected", kept, err)
	}
	call("DELETE", "/v1/desk/smoke/"+group, nil, 200)
	call("POST", "/v1/desk/turn", question, 409)
	question.SmokeID = ""
	call("POST", "/v1/desk/turn", question, 409)
	newRequest := turnRequest("虚构关闭组中的迟到检查")
	newRequest.SmokeID = group
	call("POST", "/v1/desk/turn", newRequest, 409)
	if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
		t.Fatal("closed group retry recreated data")
	}
	// Omitting the marker still creates the same ordinary source and chunk job.
	call("POST", "/v1/desk/turn", turnRequest("虚构正常对话继续"), 200)
	continued := smokeCounts(t, s, scope)
	if continued["sources"] != before["sources"]+1 || continued["memory_jobs"] != before["memory_jobs"]+1 {
		t.Fatal("normal path changed", continued)
	}
}

func TestSmokeFallbackHasNoCandidateOrSource(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "虚构普通记录"})
	before := smokeCounts(t, s, scope)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	req := turnRequest("虚构检查安排：下周晾纸。")
	req.SmokeID = string(memory.NewID())
	mustTurn(t, s, scope, req)
	during := smokeCounts(t, s, scope)
	if during["sources"] != before["sources"] || during["capture_candidates"] != before["capture_candidates"] || during["memory_jobs"] != before["memory_jobs"] {
		t.Fatal("fallback leaked", before, during)
	}
	if err := s.CleanupSmoke(context.Background(), scope, req.SmokeID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
		t.Fatal("fallback cleanup incomplete")
	}
}

func TestSmokeCannotClaimNormalConversationOrRequest(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, secretaryOutput{Reply: "普通答复。"})
	})
	req := turnRequest("虚构普通对话")
	out := mustTurn(t, s, scope, req)
	before := smokeCounts(t, s, scope)
	marked := turnRequest("虚构检查")
	marked.SmokeID = out.ConversationID
	if _, err := s.DeskTurn(context.Background(), scope, marked); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	req.SmokeID = string(memory.NewID())
	if _, err := s.DeskTurn(context.Background(), scope, req); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	if err := s.CleanupSmoke(context.Background(), scope, out.ConversationID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
		t.Fatal("cleanup touched ordinary data")
	}
}

func TestSmokeRestoresOrdinaryItemAndRejectsLaterOrdinaryEdit(t *testing.T) {
	for _, laterEdit := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "conflict"}[laterEdit], func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				secretaryModelReply(w, secretaryOutput{Reply: "改好了。", Actions: []secretaryAction{{Op: "update", Ref: "THIS", Set: map[string]json.RawMessage{"notesAppend": asJSON("虚构检查时的临时文字")}}}})
			})
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构正常档案"})
			original := state.Tasks[0]
			before := smokeCounts(t, s, scope)
			req := turnRequest("检查用的临时修改")
			req.SmokeID, req.ThingID = string(memory.NewID()), &original.ID
			out := mustTurn(t, s, scope, req)
			if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Status != "done" {
				t.Fatal("check edit did not execute", out.Turn.Receipts)
			}
			if laterEdit {
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: original.ID, Patch: asJSON(map[string]string{"notes": "虚构正常对话的新内容"})})
				beforeFailure := smokeCounts(t, s, scope)
				if err := s.CleanupSmoke(context.Background(), scope, req.SmokeID); !errors.Is(err, workspace.ErrChangedSince) {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(beforeFailure, smokeCounts(t, s, scope)) {
					t.Fatal("conflict was not atomic")
				}
				return
			}
			if err := s.CleanupSmoke(context.Background(), scope, req.SmokeID); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
				t.Fatal("restoration created memory or left documents")
			}
			after, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			got := after.Tasks[0]
			if got.Version <= original.Version {
				t.Fatal("version moved backwards")
			}
			got.Version, got.UpdatedAt = original.Version, original.UpdatedAt
			if !reflect.DeepEqual(original, got) {
				t.Fatalf("original content/history not restored: %s", asJSON(got))
			}
		})
	}
}

func TestSmokeDeputyCompletionAndLateResultCannotLeaveMemory(t *testing.T) {
	for _, tc := range []struct {
		name           string
		late, existing bool
	}{
		{"completed-new", false, false}, {"late-new", true, false}, {"completed-existing", false, true}, {"late-existing", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			late := tc.late
			ref := "new"
			if tc.existing {
				ref = "THIS"
			}
			s := testStore(t)
			scope := owner()
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					secretaryModelReply(w, secretaryOutput{Reply: "交给副手了。", Actions: []secretaryAction{{Op: "delegate", Ref: ref, Title: "虚构折纸检查", Kind: "breakdown", Prompt: "列出虚构薄纸的两个对折步骤"}}})
					return
				}
				close(started)
				if late {
					<-release
				}
				secretaryModelReply(w, "- [ ] 虚构对折一次\n- [ ] 虚构对折两次")
			})
			if _, err := s.Snapshot(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			var original string
			if tc.existing {
				state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构普通折纸事项"})
				original = state.Tasks[0].ID
			}
			before := smokeCounts(t, s, scope)
			req := turnRequest("请副手做虚构折纸的检查")
			req.SmokeID = string(memory.NewID())
			if tc.existing {
				req.ThingID = &original
			}
			out := mustTurn(t, s, scope, req)
			if len(out.State.Runs) != 1 || out.State.Runs[0].SmokeID != req.SmokeID {
				t.Fatal("run lost check marker", out.State.Runs)
			}
			done := make(chan error, 1)
			go func() { done <- s.runAgentOnce(context.Background()) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("deputy not started")
			}
			if !late {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				state, err := s.Snapshot(context.Background(), scope)
				if err != nil || state.Runs[0].Status != "done" || state.Runs[0].Adopted == nil || len(state.Tasks[0].Checklist) != 2 {
					t.Fatal("deputy did not execute and adopt", state, err)
				}
			}
			during := smokeCounts(t, s, scope)
			for _, table := range []string{"memory_records", "sources", "claims", "memory_jobs", "capture_candidates"} {
				if during[table] != before[table] {
					t.Fatal("deputy created memory", table, before, during)
				}
			}
			if err := s.CleanupSmoke(context.Background(), scope, req.SmokeID); err != nil {
				t.Fatal(err)
			}
			if late {
				// Cleanup finished while the real generation call was still in flight.
				releaseOnce.Do(func() { close(release) })
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("late worker failed to finish")
				}
			}
			if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
				t.Fatalf("deputy cleanup/late result left data: %v -> %v", before, smokeCounts(t, s, scope))
			}
		})
	}
}

func TestSmokeCleanupSerializesWithAcceptedTurn(t *testing.T) {
	s := testStore(t)
	scope := owner()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		secretaryModelReply(w, secretaryOutput{Reply: "已建。", Actions: []secretaryAction{{Op: "create_task", Title: "虚构并发检查事项"}}})
	})
	if _, err := s.Snapshot(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	before := smokeCounts(t, s, scope)
	req := turnRequest("创建虚构并发检查事项")
	req.SmokeID = string(memory.NewID())
	turnDone := make(chan error, 1)
	go func() { _, err := s.DeskTurn(context.Background(), scope, req); turnDone <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not start")
	}
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- s.CleanupSmoke(context.Background(), scope, req.SmokeID) }()
	select {
	case err := <-cleanupDone:
		t.Fatal("cleanup overtook accepted turn", err)
	case <-time.After(100 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if err := <-turnDone; err != nil {
		t.Fatal(err)
	}
	if err := <-cleanupDone; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
		t.Fatal("concurrent cleanup left data")
	}
	if _, err := s.DeskTurn(context.Background(), scope, req); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("closed retry accepted", err)
	}
}

func TestSmokeRetiredTicketCannotCreateIncompleteCandidate(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { t.Error("retired ticket called model") })
	if _, err := s.Snapshot(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	before := smokeCounts(t, s, scope)
	req := turnRequest("虚构已经过期的检查安排")
	req.SmokeID = string(memory.NewID())
	req.ConversationID = &req.SmokeID
	ctx, cancel := context.WithTimeout(withSmoke(context.Background(), req.SmokeID), 10*time.Second)
	defer cancel()
	if err := s.registerSmokeRequest(ctx, scope, req); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(asJSON(req))
	if _, err := s.admitSecretaryTurn(ctx, ctx, string(scope.OwnerID), req.RequestID, req.SmokeID, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE desk_turn_order SET expires_at=now()-interval '1 minute' WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID); err != nil {
		t.Fatal(err)
	}
	mustTurn(t, s, scope, req)
	during := smokeCounts(t, s, scope)
	if during["sources"] != before["sources"] || during["capture_candidates"] != before["capture_candidates"] || during["memory_jobs"] != before["memory_jobs"] {
		t.Fatal("retired capture leaked", during)
	}
	if err := s.CleanupSmoke(ctx, scope, req.SmokeID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
		t.Fatal("retired ticket cleanup incomplete")
	}
}

func TestSmokeUndoAndOldCheckCleanup(t *testing.T) {
	for _, undoFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "undo-first", false: "old-check"}[undoFirst], func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				secretaryModelReply(w, secretaryOutput{Reply: "已建。", Actions: []secretaryAction{{Op: "create_task", Title: "虚构旧检查事项"}}})
			})
			if _, err := s.Snapshot(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			before := smokeCounts(t, s, scope)
			req := turnRequest("创建虚构检查事项")
			req.SmokeID = string(memory.NewID())
			out := mustTurn(t, s, scope, req)
			if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].ActionID == nil {
				t.Fatal(out.Turn.Receipts)
			}
			if undoFirst {
				if _, err := s.Undo(context.Background(), scope, *out.Turn.Receipts[0].ActionID); err != nil {
					t.Fatal(err)
				}
				if got := smokeCounts(t, s, scope); got["sources"] != before["sources"] || got["memory_jobs"] != before["memory_jobs"] {
					t.Fatal("undo created check memory", got)
				}
			} else {
				if _, err := s.pool.Exec(context.Background(), "UPDATE action_log SET created_at=now()-interval '31 days',changes='[]',expired_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), *out.Turn.Receipts[0].ActionID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CleanupSmoke(context.Background(), scope, req.SmokeID); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, smokeCounts(t, s, scope)) {
				t.Fatal("undo/expired action cleanup incomplete")
			}
		})
	}
}
