package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase26HTTPUserRequests(t *testing.T, f *phase26LoadedFixture, s *Store) map[string]func(context.Context) error {
	t.Helper()
	api := httpapi.New(s, s, b1Auth{f.Scope}, s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Editor: s, Writer: s})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep the same declared light-tier foreground profile as the Store matrix.
		api.ServeHTTP(w, r.WithContext(WithMemoryTier(r.Context(), "light")))
	}))
	t.Cleanup(server.Close)
	call := func(ctx context.Context, method, path string, body any, secretary bool) error {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fictitious-phase26-acceptance")
		response, err := server.Client().Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			return fmt.Errorf("foreground HTTP %s status=%d body=%s", path, response.StatusCode, raw)
		}
		if secretary {
			var out workspace.DeskTurnResponse
			if err := json.Unmarshal(raw, &out); err != nil {
				return err
			}
			if out.Turn.Reply != "Fictitious acceptance reply." {
				return fmt.Errorf("unexpected HTTP secretary reply %q", out.Turn.Reply)
			}
		}
		return nil
	}
	return map[string]func(context.Context) error{
		"snapshot": func(ctx context.Context) error { return call(ctx, "GET", "/v1/workspace", nil, false) },
		"library": func(ctx context.Context) error {
			return call(ctx, "GET", "/v1/workspace/memory-groups/"+url.PathEscape("entity:"+string(f.Entities[0]))+"/memories?limit=50", nil, false)
		},
		"command": func(ctx context.Context) error {
			return call(ctx, "POST", "/v1/workspace/commands", workspace.Command{Type: "addTask", Title: "Fictitious HTTP concurrent task", Text: "Fictitious HTTP concurrent task", RequestID: string(memory.NewID())}, false)
		},
		"secretary": func(ctx context.Context) error {
			return call(ctx, "POST", "/v1/desk/turn", workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase26", Text: "Please answer this fictitious ordinary question."}, true)
		},
	}
}
func TestPhase26T2T4EachStageConcurrentHTTPScale(t *testing.T) {
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage} {
		t.Run(stage, func(t *testing.T) { phase26ConcurrentCase(t, stage, false, true) })
	}
}
func TestPhase26T2T4LargeMergeConcurrentHTTPScale(t *testing.T) {
	phase26ConcurrentCase(t, EntityCompareStage, true, true)
}
