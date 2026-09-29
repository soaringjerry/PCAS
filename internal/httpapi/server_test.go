package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type sourceStub struct {
	scope  memory.Scope
	called bool
	err    error
}

func (s *sourceStub) Ingest(_ context.Context, scope memory.Scope, _ memory.IngestRequest) (memory.IngestResult, error) {
	s.called = true
	s.scope = scope
	return memory.IngestResult{}, s.err
}
func (s *sourceStub) GetSource(_ context.Context, scope memory.Scope, _ memory.ID, _ int) (memory.SourceResult, error) {
	s.called = true
	s.scope = scope
	return memory.SourceResult{}, s.err
}

func TestHTTPBoundary(t *testing.T) {
	owner := memory.NewID()
	token := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, method, path, body, token string
		status                          int
		called                          bool
	}{
		{"unauthenticated", "GET", "/v1/memory/capabilities", "", "", 401, false},
		{"wrong credential", "GET", "/v1/memory/capabilities", "", "wrong", 401, false},
		{"capabilities", "GET", "/v1/memory/capabilities", "", token, 200, false},
		{"owner from server", "GET", "/v1/memory/sources/" + string(memory.NewID()), "", token, 200, true},
		{"identity injection", "POST", "/v1/memory/sources", `{"owner_id":"attacker","text":"x"}`, token, 400, false},
		{"trailing JSON", "POST", "/v1/memory/sources", `{} {}`, token, 400, false},
		{"negative version", "GET", "/v1/memory/sources/" + string(memory.NewID()) + "?version=-1", "", token, 400, false},
		{"recall not wired", "POST", "/v1/memory/recall", `{"query":"旧书店"}`, token, 501, false},
		{"expand not wired", "POST", "/v1/memory/expand", `{}`, token, 501, false},
		{"body limit", "POST", "/v1/memory/sources", `{"text":"` + strings.Repeat("x", 2<<20) + `"}`, token, 413, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &sourceStub{}
			api := New(stub, nil, NewOwnerToken(token, owner), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-ID", string(memory.NewID()))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			if w.Code != tc.status || stub.called != tc.called {
				t.Fatalf("status %d (want %d), called %v, body %s", w.Code, tc.status, stub.called, w.Body.String())
			}
			if stub.called && (stub.scope.OwnerID != owner || !stub.scope.IsOwner) {
				t.Fatal("identity was not bound by server")
			}
		})
	}
}

func TestStorageFailureDoesNotExposeData(t *testing.T) {
	token := strings.Repeat("b", 64)
	stub := &sourceStub{err: errors.New("private SQL or source content")}
	api := New(stub, nil, NewOwnerToken(token, memory.NewID()), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodGet, "/v1/memory/sources/"+string(memory.NewID()), nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}
