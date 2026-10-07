//go:build phase3_browser

package postgres

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
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Browser controller only. Unlike mock presentation probes, this serves real
// Store/HTTP commands and processors on the exact owned scale database. The
// parent runner starts it with -tags phase3_browser and closes /phase3-finish.
func TestPhase3BrowserOwnedServer(t *testing.T) {
	path := os.Getenv("PCAS_PHASE3_BROWSER_MANIFEST")
	if path == "" {
		t.Skip("browser controller requires PCAS_PHASE3_BROWSER_MANIFEST")
	}
	if !strings.HasPrefix(filepath.Clean(path), "/tmp/phase3-browser-") {
		t.Fatal("manifest must live in the runner's own temporary directory")
	}
	phase3Finding(t, "S-P3-006")
	f := phase3LoadFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f.Context = ctx
	m := phase3NewModel(t, f)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	// Secretary/deputy requests are real code. Only model output is controlled.
	// Gold text is authored independently, never inferred from generated output.
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage == "secretary" {
			return string(asJSON(map[string]any{"reply": "副手会从第二版改虚构预算。", "actions": []any{map[string]any{"op": "delegate", "ref": "THIS", "kind": "revise", "agentId": "phase3", "documentId": p.Documents[0].ID, "baseVersion": 2, "prompt": "把虚构第二版预算改为80单位，其他段落保持。"}}, "used": []any{}, "links": []any{}, "show": []any{}, "ask": nil, "memoryPlan": map[string]any{"depth": "light", "groups": []any{}}}))
		}
		if stage == "deputy" {
			return strings.ReplaceAll(p.Documents[0].Versions[1].Body, "200虚构单位", "80虚构单位")
		}
		return out
	}
	m.mu.Unlock()
	s := f.Store
	api := httpapi.New(s, s, b1Auth{f.Scope}, s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Editor: s, Writer: s})
	finished := make(chan struct{})
	var once sync.Once
	key := string(memory.NewID())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/phase3-finish" {
			if r.URL.Query().Get("key") != key {
				w.WriteHeader(403)
				return
			}
			once.Do(func() { close(finished) })
			w.WriteHeader(204)
			return
		}
		api.ServeHTTP(w, r.WithContext(WithMemoryTier(r.Context(), "light")))
	}))
	defer server.Close()
	runnerDone := make(chan struct{})
	go func() { defer close(runnerDone); _ = s.RunAgents(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	manifest := map[string]any{"ownedDisposable": true, "backendURL": server.URL, "finishKey": key, "projectId": p.ID, "documentId": p.Documents[0].ID, "goldVersion2": p.Documents[0].Versions[1].Body, "goldVersion3": strings.ReplaceAll(p.Documents[0].Versions[1].Body, "200虚构单位", "80虚构单位")}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Error("browser controller timed out")
	}
	cancel()
	<-runnerDone
}
