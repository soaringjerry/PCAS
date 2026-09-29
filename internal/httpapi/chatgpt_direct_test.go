package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type scopedAuth struct{ scope memory.Scope }

func (a scopedAuth) Authenticate(*http.Request) (memory.Scope, bool) { return a.scope, true }

func TestDirectChatGPTHTTPBoundary(t *testing.T) {
	m, err := siwc.New(t.TempDir(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	models, err := ai.Load("", nil, m)
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.NewID()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name, method, path, body string
		isOwner                  bool
		want                     int
	}{
		{"account owner", "GET", "/v1/chatgpt/direct/account", "", true, 200},
		{"agent cannot manage OAuth", "POST", "/v1/chatgpt/direct/login", "{}", false, 403},
		{"no tokens returned", "GET", "/v1/chatgpt/direct/account", "", false, 403},
		{"model unauthenticated", "GET", "/v1/chatgpt/direct/models", "", true, 501},
		{"unknown credential fields", "POST", "/v1/chatgpt/direct/login", `{"access_token":"secret"}`, true, 400},
		{"missing registration", "POST", "/v1/chatgpt/direct/select", `{"client_id":"missing"}`, true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := New(&sourceStub{}, nil, scopedAuth{memory.Scope{OwnerID: owner, PrincipalID: "test", IsOwner: test.isOwner}}, func(context.Context) error { return nil }, logger, Options{Models: models})
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer test")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("account response could be cached")
			}
			for _, secret := range []string{"access_token", "refresh_token", "id_token", "ext_agent_host_id"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("credential field exposed")
				}
			}
		})
	}
}
func TestUpstreamAuthErrorDoesNotLogOutPCAS(t *testing.T) {
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	w := httptest.NewRecorder()
	s.fail(w, &siwc.ProviderError{Status: 401, Code: "subscription_sharing_invalid_user", RequestID: "req-safe", Body: []byte(`{"detail":"PRIVATE"}`), BodyShape: "detail"})
	if w.Code == 401 || w.Code != 502 || strings.Contains(w.Body.String(), "PRIVATE") || !strings.Contains(w.Body.String(), "req-safe") {
		t.Fatalf("unsafe upstream error: %d %s", w.Code, w.Body.String())
	}
}
