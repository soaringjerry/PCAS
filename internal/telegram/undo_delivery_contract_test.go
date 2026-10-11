package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// U15 crosses the real command API and real PostgreSQL Undo boundary. The bot
// and secretary model are local fixtures shared with existing channel tests.
func TestTelegramWebThenTelegramAlreadyUndone(t *testing.T) {
	s := integrationStore(t)
	p, _, bot := fixture(t)
	p.store = s
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"reply":"安排好了","actions":[{"op":"create_task","title":"T1 cross-channel"}]}`}}}})
	}))
	t.Cleanup(model.Close)
	s.SetModels(&ai.Registry{HTTP: model.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "T1 local", Protocol: "openai", BaseURL: model.URL, Model: "test", MaxOutput: 1000, CostMode: "free"}}}})
	bot.enqueue(textUpdate(1, "创建跨渠道撤销事项"))
	step(t, p)
	sent := bot.of("sendMessage")
	if len(sent) != 1 {
		t.Fatal("missing secretary receipt", sent)
	}
	rows := decode[keyboard](sent[0].body["reply_markup"]).Rows
	if len(rows) != 1 || len(rows[0]) != 1 || !strings.HasPrefix(rows[0][0].Data, "u:") {
		t.Fatal("missing undo button", rows)
	}
	action := strings.TrimPrefix(rows[0][0].Data, "u:")
	st, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Tasks) != 1 {
		t.Fatal("missing task", err, st.Tasks)
	}
	token := strings.Repeat("T1", 32)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, p.scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	body, _ := json.Marshal(workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
	request := httptest.NewRequest("POST", "/v1/workspace/commands", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("web undo failed", response.Code, response.Body.String())
	}
	before, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(before.Tasks) != 0 || len(before.Notices) != 0 || len(before.Docs) != 0 || len(before.Runs) != 0 || len(before.Samples) != 0 {
		t.Fatal("web undo retained business rows", err, before)
	}
	bot.enqueue(callbackUpdate(2, rows[0][0].Data, 101))
	step(t, p)
	answers := bot.of("answerCallbackQuery")
	if len(answers) != 1 || decode[string](answers[0].body["text"]) != "已经撤销过了。" {
		t.Fatal("wrong cross-channel response", answers)
	}
	after, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || !reflect.DeepEqual(before, after) || calls.Load() != 1 {
		t.Fatal("Telegram callback changed state or called model", err, calls.Load(), before.Revision, after.Revision)
	}
}
