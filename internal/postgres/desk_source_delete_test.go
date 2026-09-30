package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryOriginalSourceDeletionScrubsHistoryAndReplay(t *testing.T) {
	for _, connector := range []string{"desk", "capture"} {
		t.Run(connector, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			scope := owner()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if connector == "capture" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				secretaryModelReply(w, `{"reply":"安排已记下","links":["https://example.com"],"remember":true,"actions":[{"op":"create_task","title":"检查安排"}],"ask":{"question":"确认安排？","options":["确认"]}}`)
			})
			req := turnRequest("原话暗号紫色山谷")
			// ExternalID retains the caller's UUID spelling; request_id is a UUID column.
			req.RequestID = strings.ToUpper(req.RequestID)
			first := mustTurn(t, s, scope, req)
			otherReq := turnRequest("本对话未删除的话")
			otherReq.ConversationID = &first.ConversationID
			other := mustTurn(t, s, scope, otherReq)
			otherOwner := owner()
			isolated := mustTurn(t, s, otherOwner, req)
			var sourceID string
			var version int
			var dependencies []byte
			if err := s.pool.QueryRow(ctx, "SELECT s.id::text,r.version,t.dependencies FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) JOIN desk_turns t ON t.owner_id=s.owner_id AND t.request_id::text=lower(s.external_id) WHERE s.owner_id=$1 AND s.connector=$3 AND s.external_id=$2", string(scope.OwnerID), req.RequestID, connector).Scan(&sourceID, &version, &dependencies); err != nil {
				t.Fatal(err)
			}
			if string(dependencies) != "[]" || len(first.Turn.Receipts) == 0 {
				t.Fatal("fixture must exercise deletion without memory dependencies", string(dependencies), first.Turn)
			}
			// An unrelated connector may use the same ExternalID; it must not erase the turn.
			unrelated := mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: req.RequestID, ExternalVersion: "1", Text: "普通资料", MediaType: "text/plain"})
			if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: unrelated.ID, Version: unrelated.Version, Kind: memory.SourceKind}}}); err != nil {
				t.Fatal(err)
			}
			if replay := mustTurn(t, s, scope, req); replay.Turn.Text != req.Text {
				t.Fatal("unrelated source erased turn", replay.Turn)
			}
			if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(sourceID), Version: version, Kind: memory.SourceKind}}, BlockReimport: true}); err != nil {
				t.Fatal(err)
			}
			var question, answer string
			var stored []byte
			if err := s.pool.QueryRow(ctx, "SELECT question,answer,response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&question, &answer, &stored); err != nil {
				t.Fatal(err)
			}
			if question != "" || answer != "" || strings.Contains(string(stored), req.Text) {
				t.Fatal("original input retained in storage", question, answer, string(stored))
			}
			token := strings.Repeat("f", 64)
			api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
			for _, endpoint := range []struct{ method, path string }{{"GET", "/v1/desk/turns?conversationId=" + first.ConversationID}, {"POST", "/v1/desk/turn"}} {
				request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(string(asJSON(req))))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				api.ServeHTTP(w, request)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), req.Text) {
					t.Fatal(endpoint, w.Code, w.Body.String())
				}
				var turn workspace.SecretaryTurn
				if endpoint.method == "GET" {
					var history workspace.DeskTurnsResponse
					if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Turns) != 2 || history.Turns[1].ID != other.Turn.ID || history.Turns[1].Text != otherReq.Text {
						t.Fatal("unrelated turn changed", err, w.Body.String())
					}
					turn = history.Turns[0]
				} else {
					var replay workspace.DeskTurnResponse
					if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.ConversationID != first.ConversationID {
						t.Fatal(err, w.Body.String())
					}
					turn = replay.Turn
					if turn.Reply != "" {
						t.Fatal("replay retained reply", turn)
					}
				}
				if turn.ID != first.Turn.ID || turn.Text != "" || turn.Cards == nil || len(turn.Cards) != 0 || turn.Ask != nil || len(turn.Receipts) != len(first.Turn.Receipts) {
					t.Fatal("turn not scrubbed", turn)
				}
				for i, receipt := range turn.Receipts {
					original := first.Turn.Receipts[i]
					if receipt.Text != "（内容已删除）" || pointerValue(receipt.ActionID) != pointerValue(original.ActionID) || receipt.Op != original.Op || receipt.Status != original.Status {
						t.Fatal("receipt not scrubbed or audit metadata lost", receipt, original)
					}
				}
			}
			if replay := mustTurn(t, s, otherOwner, req); replay.Turn.ID != isolated.Turn.ID || replay.Turn.Text != req.Text {
				t.Fatal("deletion crossed owner boundary", replay.Turn)
			}
			if calls.Load() != 3 {
				t.Fatal("replay reran model", calls.Load())
			}
		})
	}
}
