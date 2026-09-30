package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestDeskRouteBoundary(t *testing.T) {
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{"intent":{"type":"choice","choice":"ask","confidence":0.8}}}`))
	}))
	defer jev.Close()
	router := ai.NewRouter(func() string { return "k" })
	router.BaseURL = jev.URL
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name   string
		owner  bool
		router *ai.Router
		body   string
		status int
	}{
		{"routes", true, router, `{"text":"我上次体检是哪天"}`, 200},
		{"no model falls back", true, nil, `{"text":"x"}`, http.StatusNotImplemented},
		{"empty", true, router, `{"text":"  "}`, http.StatusBadRequest},
		{"owner only", false, router, `{"text":"x"}`, http.StatusForbidden},
	} {
		handler := New(&sourceStub{}, nil, scopedAuth{memory.Scope{OwnerID: memory.NewID(), PrincipalID: "test", IsOwner: tc.owner}}, func(context.Context) error { return nil }, logger, Options{Router: tc.router})
		req := httptest.NewRequest(http.MethodPost, "/v1/desk/route", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s: status %d", tc.name, rec.Code)
		}
		if tc.status == 200 && !strings.Contains(rec.Body.String(), `"intent":"ask"`) {
			t.Fatalf("%s: body %s", tc.name, rec.Body.String())
		}
	}
}

func TestDecisionKeySettings(t *testing.T) {
	var sent string
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"answers":{"intent":{"type":"choice","choice":"delegate","confidence":0.9}}}`))
	}))
	defer jev.Close()
	models := &ai.Registry{SettingsPath: filepath.Join(t.TempDir(), "api.json")}
	router := ai.NewRouter(models.DecisionKey)
	router.BaseURL = jev.URL
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	call := func(owner bool, method, body string) *httptest.ResponseRecorder {
		handler := New(&sourceStub{}, nil, scopedAuth{memory.Scope{OwnerID: memory.NewID(), PrincipalID: "test", IsOwner: owner}}, func(context.Context) error { return nil }, logger, Options{Models: models, Router: router})
		req := httptest.NewRequest(method, "/v1/models/decision", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := call(false, "POST", `{"api_key":"SECRET_VALUE"}`); rec.Code != 403 {
		t.Fatalf("non-owner saved a key: %d", rec.Code)
	}
	if rec := call(true, "POST", `{"api_key":"  "}`); rec.Code != 400 {
		t.Fatalf("blank key: %d", rec.Code)
	}
	rec := call(true, "POST", `{"api_key":"SECRET_VALUE"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"working":true`) || strings.Contains(rec.Body.String(), "SECRET_VALUE") || sent != "Bearer SECRET_VALUE" {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(true, "DELETE", ""); rec.Code != 200 || router.Configured() {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
}
