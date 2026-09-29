package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestModelSettingsOwnerAndSecretBoundary(t *testing.T) {
	models := &ai.Registry{SettingsPath: filepath.Join(t.TempDir(), "api.json")}
	for _, tc := range []struct {
		method, path, body string
		owner              bool
		want               int
	}{
		{"POST", "/v1/models/openai/text", `{"base_url":"https://example.invalid/v1","model":"gpt-6.1-sol","api_key":"SECRET_VALUE","input_cny_per_million":1,"output_cny_per_million":4}`, false, 403},
		{"GET", "/v1/models/openai", "", false, 403},
		{"POST", "/v1/models/openai/text", `{"base_url":"https://example.invalid/v1","model":"gpt-6.1-sol","api_key":"SECRET_VALUE","input_cny_per_million":1,"output_cny_per_million":4}`, true, 200},
		{"GET", "/v1/models/openai", "", true, 200},
		{"GET", "/v1/models", "", true, 200},
		{"POST", "/v1/models/openai/unknown", `{}`, true, 400},
		{"POST", "/v1/models/openai/text", `{"base_url":"https://user:pass@example.invalid/v1","model":"test","api_key":"SECRET_VALUE","input_cny_per_million":1}`, true, 400},
		{"POST", "/v1/models/embeddings/rebuild", `{}`, false, 403},
	} {
		handler := New(&sourceStub{}, nil, scopedAuth{memory.Scope{OwnerID: memory.NewID(), PrincipalID: "test", IsOwner: tc.owner}}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{Models: models})
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SECRET_VALUE") || strings.Contains(w.Body.String(), "api_key") {
			t.Fatal("credential returned to client")
		}
	}
}
