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
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestDeskRouteBoundary(t *testing.T) {
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{"intent":{"type":"choice","choice":"ask","confidence":0.8}}}`))
	}))
	defer jev.Close()
	router := ai.NewRouter("k")
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
