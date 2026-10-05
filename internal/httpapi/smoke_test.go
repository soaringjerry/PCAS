package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type smokeDeskStub struct {
	workspace.API
	turns, cleanups int
}

func (s *smokeDeskStub) DeskTurn(_ context.Context, _ memory.Scope, in workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	s.turns++
	return workspace.DeskTurnResponse{ConversationID: in.SmokeID}, nil
}
func (s *smokeDeskStub) CleanupSmoke(context.Context, memory.Scope, string) error {
	s.cleanups++
	return nil
}

func TestSmokeRequiresOwnerBearerAndLeavesNormalCookieTurnsAlone(t *testing.T) {
	ownerToken, agentToken := strings.Repeat("synthetic-owner-", 3), strings.Repeat("synthetic-agent-", 3)
	t.Setenv("PCAS_SMOKE_TEST_AGENT", agentToken)
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`[{"principal":"synthetic-helper","token_env":"PCAS_SMOKE_TEST_AGENT"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	auth, err := LoadCredentials(ownerToken, memory.NewID(), path)
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessions(auth)
	login := httptest.NewRequest("POST", "/login", strings.NewReader(`{"token":"`+ownerToken+`"}`))
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	sessions.Login(w, login)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("owner login failed", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	stub := &smokeDeskStub{}
	api := New(&sourceStub{}, nil, sessions, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{Workspace: stub})
	for _, tc := range []struct {
		name, token string
		cookie      bool
		status      int
	}{
		{"owner bearer", ownerToken, false, 200},
		{"agent bearer", agentToken, false, 403},
		{"owner cookie", "", true, 403},
		{"unauthenticated", "", false, 401},
		{"invalid bearer with valid cookie", "invalid-synthetic-token", true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{"POST", "DELETE"} {
				group := string(memory.NewID())
				path, body := "/v1/desk/turn", `{"smokeId":"`+group+`","requestId":"`+string(memory.NewID())+`","text":"虚构检查问题"}`
				if method == "DELETE" {
					path, body = "/v1/desk/smoke/"+group, ""
				}
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				if tc.cookie {
					req.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				api.ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
				}
			}
		})
	}
	if stub.turns != 1 || stub.cleanups != 1 {
		t.Fatal("unauthorized call reached store", stub)
	}
	normal := httptest.NewRequest(http.MethodPost, "/v1/desk/turn", strings.NewReader(`{"requestId":"`+string(memory.NewID())+`","text":"普通虚构对话"}`))
	normal.Header.Set("Content-Type", "application/json")
	normal.AddCookie(cookie)
	w = httptest.NewRecorder()
	api.ServeHTTP(w, normal)
	if w.Code != 200 || stub.turns != 2 {
		t.Fatal("ordinary cookie turn affected", w.Code)
	}
}

func TestSmokeOmittedMarkerPreservesRequestEncoding(t *testing.T) {
	// Existing request hashes must remain replayable across this migration.
	in := workspace.DeskTurnRequest{RequestID: "synthetic-request", Text: "普通虚构对话"}
	b, err := json.Marshal(in)
	want := `{"requestId":"synthetic-request","conversationId":null,"thingId":null,"text":"普通虚构对话","agentId":""}`
	if err != nil || string(b) != want {
		t.Fatal(string(b), err)
	}
}
