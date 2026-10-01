package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func stabilizationDeletionAssertScrubbed(t *testing.T, s *Store, scope memory.Scope, requestID, secret string) {
	t.Helper()
	var question, answer string
	var response []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT question,answer,response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), requestID).Scan(&question, &answer, &response); err != nil {
		t.Fatal(err)
	}
	if question != "" || answer != "" || strings.Contains(string(response), secret) {
		t.Errorf("deleted text retained: questionEmpty=%v answerEmpty=%v cachedSecret=%v", question == "", answer == "", strings.Contains(string(response), secret))
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["state"]; ok {
		t.Fatal("cache contains entire workspace state")
	}
}

func TestStabilizationD1_ReferencedMemoryDeletionClearsWholeTurn(t *testing.T) {
	s, scope := testStore(t), owner()
	logs := secretaryLogs(t)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"D1引用暗号银色森林","used":["M1"],"links":["https://example.com"],"actions":[{"op":"create_task","title":"D1普通事项"}],"ask":{"question":"银色森林在哪里？","options":["秘密地点"]}}`)
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "D1引用暗号银色森林"})
	originalSource := st.Candidates[0].Source
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "D1引用暗号银色森林"})
	id := st.Memories[0].ID

	req := turnRequest("D1原话也提到银色森林")
	first := mustTurn(t, s, scope, req)
	if len(first.Turn.Cards) == 0 || len(first.Turn.Receipts) != 1 {
		t.Fatal("fixture did not cite memory")
	}
	var dependencies []memory.Ref
	var rawDependencies []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&rawDependencies); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawDependencies, &dependencies); err != nil {
		t.Fatal(err)
	}
	cited := false
	for _, card := range first.Turn.Cards {
		if card.Kind == "sources" && strings.Contains(string(asJSON(card.Items)), id) {
			cited = true
		}
	}
	if !cited || !hasArtifactDependency(dependencies, id) {
		t.Fatal("fixture did not cite the authorized claim or persist its dependency")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: id})
	for _, m := range st.Memories {
		if m.ID == id {
			t.Fatal("claim was not deleted")
		}
	}
	var retainedSource string
	if err := s.pool.QueryRow(context.Background(), "SELECT body FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), originalSource.SourceID, originalSource.Version).Scan(&retainedSource); err != nil || !strings.Contains(retainedSource, "银色森林") {
		t.Fatal("claim deletion removed independent source without authorization", err)
	}
	stabilizationDeletionAssertScrubbed(t, s, scope, req.RequestID, "银色森林")
	replay := mustTurn(t, s, scope, req)
	if replay.Turn.Text != "" || replay.Turn.Reply != "" || len(replay.Turn.Cards) != 0 || replay.Turn.Ask != nil || len(replay.Turn.Receipts) != 1 || replay.Turn.Receipts[0].Text != "（内容已删除）" {
		t.Error("replay did not retain only receipt skeleton")
	}
	if replay.Turn.Receipts[0].ActionID == nil || *replay.Turn.Receipts[0].ActionID != *first.Turn.Receipts[0].ActionID || replay.Turn.Receipts[0].Status != first.Turn.Receipts[0].Status {
		t.Fatal("audit skeleton lost")
	}
	history, err := s.DeskTurns(context.Background(), scope, first.ConversationID)
	if err != nil || len(history.Turns) != 1 || strings.Contains(string(asJSON(history)), "银色森林") {
		t.Fatal("history leaked deleted memory", err)
	}
	if strings.Contains(logs.String(), "银色森林") {
		t.Fatal("deleted text retained in logs")
	}
}

func TestStabilizationD2_OriginalSourceDeletionClearsWholeTurn(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"D2私密原話已安排","actions":[{"op":"create_task","title":"D2普通事项"}],"ask":{"question":"确认私密原話？","options":["确认"]}}`)
	})
	req := turnRequest("D2私密原話")
	first := mustTurn(t, s, scope, req)
	var sourceID string
	var version int
	if err := s.pool.QueryRow(context.Background(), "SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk' AND s.external_id=$2", string(scope.OwnerID), req.RequestID).Scan(&sourceID, &version); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(sourceID), Version: version, Kind: memory.SourceKind}}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	stabilizationDeletionAssertScrubbed(t, s, scope, req.RequestID, "私密原話")
	history, err := s.DeskTurns(context.Background(), scope, first.ConversationID)
	if err != nil || len(history.Turns) != 1 || history.Turns[0].Text != "" || history.Turns[0].Ask != nil || strings.Contains(string(asJSON(history)), "私密原話") {
		t.Fatal("original remains in history", err)
	}
}

func TestStabilizationD3_DeletedSourceExpiresRelatedUndoSnapshot(t *testing.T) {
	s, scope := testStore(t), owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "D3需要删掉的正文"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "task", Text: "D3来源事项"})
	if len(st.Tasks) != 1 || len(st.Tasks[0].Sources) == 0 {
		t.Fatal("fixture lacks source-linked task")
	}
	id := st.Tasks[0].ID
	source := st.Tasks[0].Sources[0]
	action := string(memory.NewID())
	st, err := s.Execute(context.Background(), scope, workspace.Command{Type: "setNotes", ID: id, Text: "D3修改", RequestID: action, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(source.SourceID), Version: source.Version, Kind: memory.SourceKind}}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	var changes []byte
	var expired bool
	if err = s.pool.QueryRow(context.Background(), "SELECT changes,expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&changes, &expired); err != nil || string(changes) != "[]" || !expired {
		t.Fatal("deleted snapshot retained", err, expired)
	}
	t.Run("expired_code", func(t *testing.T) {
		stabilizationSecretaryFinding(t, "T3-D3/T1-U12: erased snapshot returns changed_since instead of expired; propagation passes")
		// Propagation assertions above are independent of T1-U12's undo-state
		// coverage. Only the public error-code check overlaps that finding.
		st, err := s.Snapshot(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		token := strings.Repeat("t", 64)
		api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
		command := workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: st.Revision}
		request := httptest.NewRequest("POST", "/v1/workspace/commands", strings.NewReader(string(asJSON(command))))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 409 || body.Error != "expired" {
			t.Fatalf("want 409 expired for erased snapshot, got %d %s", response.Code, body.Error)
		}
	})
}

func TestStabilizationD4_ReplayCannotRestoreDeletedOriginal(t *testing.T) {
	for _, mode := range []string{"success", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			logs := secretaryLogs(t)
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "fallback" {
					http.Error(w, "private-model-output", 500)
					return
				}
				secretaryModelReply(w, `{"reply":"D4原话紫色山谷","actions":[{"op":"create_task","title":"D4普通事项"}]}`)
			})
			req := turnRequest("D4原话紫色山谷")
			first := mustTurn(t, s, scope, req)
			connector := "desk"
			wantTasks := 1
			if mode == "fallback" {
				connector = "capture"
				wantTasks = 0
			}
			var sourceID string
			var version int
			if err := s.pool.QueryRow(context.Background(), "SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3", string(scope.OwnerID), connector, req.RequestID).Scan(&sourceID, &version); err != nil {
				t.Fatal(err)
			}
			if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(sourceID), Version: version, Kind: memory.SourceKind}}, BlockReimport: true}); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			replay := mustTurn(t, s, scope, req)
			if replay.Turn.ID != first.Turn.ID || calls.Load() != before || strings.Contains(string(asJSON(replay)), "紫色山谷") || strings.Contains(logs.String(), "紫色山谷") {
				t.Fatal("deleted input restored or re-executed")
			}
			stabilizationDeletionAssertScrubbed(t, s, scope, req.RequestID, "紫色山谷")
			stabilizationSecretaryTasks(t, s, scope, wantTasks)
		})
	}
}

func TestStabilizationD1_DeletionDuringGenerationCannotCommitDerivedText(t *testing.T) {
	s, scope := testStore(t), owner()
	entered, release := make(chan bool, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.Add(1)
		entered <- strings.Contains(string(body), "私密红色地平线")
		<-release
		secretaryModelReply(w, `{"reply":"私密红色地平线","used":["M1"],"actions":[{"op":"create_task","title":"私密红色地平线"}]}`)
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "F11删除测试 私密红色地平线"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "F11删除测试 私密红色地平线"})
	id := st.Memories[0].ID
	req := turnRequest("F11删除测试")
	type result struct {
		out workspace.DeskTurnResponse
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := s.DeskTurn(context.Background(), scope, req); done <- result{out, err} }()
	select {
	case shown := <-entered:
		if !shown {
			unblock()
			t.Fatal("model did not receive deletion target")
		}
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("model did not enter")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: id})
	unblock()
	var out workspace.DeskTurnResponse
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		out = got.out
	case <-time.After(5 * time.Second):
		t.Fatal("deletion blocked generation completion")
	}
	if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" || !strings.Contains(out.Turn.Receipts[0].Text, "上下文已变更") || strings.Contains(string(asJSON(out.Turn)), "私密红色地平线") {
		t.Fatal("deleted model context committed", out.Turn)
	}
	stabilizationSecretaryTasks(t, s, scope, 0)
	replay := mustTurn(t, s, scope, req)
	if calls.Load() != 1 || strings.Contains(string(asJSON(replay.Turn)), "私密红色地平线") {
		t.Fatal("replay regenerated deleted content")
	}
	var cache []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&cache); err != nil || strings.Contains(string(cache), "私密红色地平线") {
		t.Fatal("derived text persisted after concurrent deletion", err)
	}
}
