package postgres

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const phase2ManualProvider = "phase2-manual-destination"

// Configure only the destination. Each old test must explicitly choose it in
// requestRun and perform its own real package GET before submitting a result.
func phase2ManualDestination(t *testing.T, s *Store) {
	t.Helper()
	if s.models != nil {
		if _, ok := s.models.Get(phase2ManualProvider); ok {
			return
		}
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("manual handoff invoked its external destination")
		http.Error(w, "manual does not dispatch", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if calls.Load() != 0 {
			t.Errorf("manual target HTTP requests=%d, want 0", calls.Load())
		}
	})
	registry := s.models
	if registry == nil {
		registry = &ai.Registry{}
	}
	registry.Config.Providers = append(registry.Config.Providers, ai.Provider{ID: phase2ManualProvider, Name: "合成人工目标", Protocol: "openai", BaseURL: server.URL, Model: "phase2-manual-fixed", CostMode: "free", MaxOutput: 100})
	s.SetModels(registry)
}

func TestPhase2ManualMissingDestinationCannotPrepareOrDeliver(t *testing.T) {
	if os.Getenv("PCAS_TEST_DATABASE_URL") == "" {
		t.Fatal("independent manual acceptance requires disposable database; no skip")
	}
	s := testStore(t)
	phase2ManualDestination(t, s)
	scope := owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "明确人工目标边界"})
	requestID := string(memory.NewID())
	_, err := s.Execute(context.Background(), scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "manual", Kind: "draft", Prompt: "合成普通正文", RequestID: requestID, ExpectedRevision: st.Revision})
	if !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("missing destination error=%v, want exact Invalid", err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Runs) != 0 {
		t.Fatal("missing destination created a run", err, state.Runs)
	}
	var attempts int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("missing destination retained attempt count=%d error=%v", attempts, err)
	}
}
