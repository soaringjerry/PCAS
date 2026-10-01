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

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The literal evidence atoms were committed in afd30a6 before the consumer
// implementation was read. Fixture interfaces and capture code are adapters;
// they do not create expected answers from Recall, Brief, or product output.
type phase2Gold struct {
	ProductSHA string `json:"product_sha"`
	Records    []struct {
		ID              string   `json:"id"`
		ExternalVersion string   `json:"external_version"`
		Text            string   `json:"text"`
		Claim           string   `json:"claim"`
		DerivedOutput   string   `json:"derived_output"`
		Atoms           []string `json:"atoms"`
	} `json:"records"`
	Cases []struct {
		ID       string   `json:"id"`
		Query    string   `json:"query"`
		Required []string `json:"required"`
	} `json:"cases"`
}

func phase2ReadGold(t *testing.T) phase2Gold {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold phase2Gold
	if err := json.Unmarshal(data, &gold); err != nil {
		t.Fatal(err)
	}
	if len(gold.Records) != 3 || len(gold.Cases) != 4 {
		t.Fatal("frozen gold shape changed")
	}
	return gold
}

func phase2Store(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("PCAS_TEST_DATABASE_URL") == "" {
		t.Fatal("phase2 acceptance requires a dedicated disposable PCAS_TEST_DATABASE_URL; no skip")
	}
	return testStore(t)
}

func phase2Evidence(t *testing.T, suffix string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s=%s", suffix, data)
	if dir := os.Getenv("PCAS_PHASE2_EVIDENCE_DIR"); dir != "" {
		if !strings.HasPrefix(filepath.Clean(dir), "/tmp/pcas-phase2-c-") {
			t.Fatal("evidence directory must be a dedicated /tmp/pcas-phase2-c-* directory")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(t.Name(), "/", "__") + "-" + suffix + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type phase2Capture struct {
	mu     sync.Mutex
	bodies []json.RawMessage
	paths  []string
}

func phase2Model(t *testing.T, s *Store) *phase2Capture {
	t.Helper()
	capture := &phase2Capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			t.Error(err)
			http.Error(w, "capture failed", 500)
			return
		}
		capture.mu.Lock()
		capture.bodies = append(capture.bodies, append(json.RawMessage(nil), body...))
		capture.paths = append(capture.paths, r.URL.Path)
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// This response intentionally claims no memory use; gold comes only from
		// actual request bytes, never from this generated answer.
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"reply":"合成验收回执","used":[],"links":[],"show":[],"remember":false,"actions":[],"ask":null}`}}}, "usage": map[string]int{"prompt_tokens": 120, "completion_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "phase2-model", Providers: []ai.Provider{{ID: "phase2-model", Name: "phase2 synthetic model", Protocol: "openai", BaseURL: server.URL, Model: "phase2-synthetic", MaxOutput: 200, CostMode: "free"}}}})
	return capture
}

func (c *phase2Capture) last(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		t.Fatal("actual entry did not dispatch a provider HTTP request")
	}
	if len(c.bodies) != 1 {
		t.Errorf("want one generation, got %d HTTP requests", len(c.bodies))
	}
	body := c.bodies[len(c.bodies)-1]
	phase2Evidence(t, "provider-request", map[string]any{"path": c.paths[len(c.paths)-1], "body": body, "layer": "fake provider HTTP receive", "third_party_internal_context": "unknown"})
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	for _, message := range payload.Messages {
		if message.Role == "system" || message.Role == "user" {
			input.WriteString(message.Content)
			input.WriteByte('\n')
		}
	}
	return input.String()
}

func phase2GrantSQL(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) {
	t.Helper()
	for _, agent := range []string{"phase2-model", "manual"} {
		if _, err := s.pool.Exec(context.Background(), "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)", string(scope.OwnerID), string(ref.ID), agent); err != nil {
			t.Fatal(err)
		}
	}
}

func phase2Atoms(t *testing.T, input string, atoms []string) {
	t.Helper()
	for _, atom := range atoms {
		if !strings.Contains(input, atom) {
			t.Errorf("required evidence atom absent from actual input: %q", atom)
		}
	}
}

func phase2RefPresent(refs []memory.Ref, ref memory.Ref) bool {
	for _, got := range refs {
		if got == ref {
			return true
		}
	}
	return false
}

func phase2Run(t *testing.T, s *Store, scope memory.Scope, capture *phase2Capture, agent, query string) (workspace.Run, string) {
	t.Helper()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "独立验收任务"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: agent, Kind: "ask", Prompt: query})
	if len(state.Runs) != 1 {
		t.Fatalf("want one run, got %d", len(state.Runs))
	}
	run := state.Runs[0]
	phase2Evidence(t, "prepared-run", run)
	if agent == "manual" {
		if run.Status != "waiting" {
			t.Errorf("manual package status=%q, want waiting", run.Status)
		}
		return run, run.Brief
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.RunAgents(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	// Observe one queued run to completion. This is a bounded state wait, not
	// a repeated test or a repeated model request.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("RunAgents did not finish synthetic queued job")
		case <-ticker.C:
			current, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range current.Runs {
				if got.ID == run.ID && (got.Status == "done" || got.Status == "failed") {
					phase2Evidence(t, "finished-run", got)
					if got.Status != "done" {
						t.Errorf("run did not complete: %+v", got)
					}
					return got, capture.last(t)
				}
			}
		}
	}
}

func TestPhase2ZeroClaimsRawActualInputs(t *testing.T) {
	gold := phase2ReadGold(t)
	for _, entry := range []string{"DeskTurn", "RunAgents", "manual"} {
		t.Run(entry, func(t *testing.T) {
			s, scope := phase2Store(t), owner()
			capture := phase2Model(t, s)
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
			record := gold.Records[0]
			source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-synthetic", ExternalID: record.ID, ExternalVersion: record.ExternalVersion, Title: "成都预约资料", Text: record.Text, MediaType: "text/plain"})
			phase2GrantSQL(t, s, scope, source.Ref)
			var claims, pending int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&claims); err != nil {
				t.Fatal(err)
			}
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND state='queued'", string(scope.OwnerID), string(source.ID)).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if claims != 0 || pending == 0 {
				t.Fatalf("raw fixture requires zero claims and pending processing: claims=%d pending=%d", claims, pending)
			}
			query := gold.Cases[0].Query
			agent := "phase2-model"
			if entry == "manual" {
				agent = "manual"
			}
			recall, err := s.Recall(context.Background(), memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent}, memory.RecallRequest{Query: query, Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}})
			if err != nil {
				t.Fatal(err)
			}
			phase2Evidence(t, "recall-diagnostic", recall)
			if !phase2RefPresent(recall.Memories, source.Ref) {
				t.Error("authorized zero-claim source absent from Recall diagnostic")
			}
			if _, err := s.GetSource(context.Background(), memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent}, source.ID, source.Version); err != nil {
				t.Fatal("SQL-authorized source is not readable:", err)
			}
			var refs []memory.Ref
			var input string
			if entry == "DeskTurn" {
				out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: agent, Text: query})
				if err != nil {
					t.Fatal(err)
				}
				phase2Evidence(t, "desk-response", out.Turn)
				if err := s.pool.QueryRow(context.Background(), "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.Turn.ID).Scan(&refs); err != nil {
					t.Fatal(err)
				}
				input = capture.last(t)
			} else {
				run, actual := phase2Run(t, s, scope, capture, agent, query)
				refs, input = run.ContextVersions, actual
			}
			phase2Evidence(t, "dependencies", refs)
			phase2Atoms(t, input, gold.Cases[0].Required)
			if !phase2RefPresent(refs, source.Ref) {
				t.Error("actual source input must carry exact typed source/version dependency")
			}
		})
	}
}

func TestPhase2ClaimControlUsedEmptyActualInputs(t *testing.T) {
	gold := phase2ReadGold(t)
	for _, entry := range []string{"DeskTurn", "RunAgents", "manual"} {
		t.Run(entry, func(t *testing.T) {
			s, scope := phase2Store(t), owner()
			capture := phase2Model(t, s)
			record := gold.Records[1]
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: record.Text})
			state = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: record.Claim})
			if len(state.Memories) != 1 || state.Memories[0].Confirmation != "confirmed" {
				t.Fatal("claim positive control was not confirmed")
			}
			ref := memory.Ref{ID: memory.ID(state.Memories[0].ID), Version: state.Memories[0].Version, Kind: memory.ClaimKind}
			query := gold.Cases[2].Query
			var refs []memory.Ref
			var input string
			if entry == "DeskTurn" {
				out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: query})
				if err != nil {
					t.Fatal(err)
				}
				phase2Evidence(t, "desk-response-used-empty", out.Turn)
				for _, card := range out.Turn.Cards {
					if card.Kind == "sources" {
						t.Error("Used=[] fixture unexpectedly has source citation card")
					}
				}
				if err := s.pool.QueryRow(context.Background(), "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.Turn.ID).Scan(&refs); err != nil {
					t.Fatal(err)
				}
				input = capture.last(t)
			} else {
				agent := "phase2-model"
				if entry == "manual" {
					agent = "manual"
				}
				run, actual := phase2Run(t, s, scope, capture, agent, query)
				refs, input = run.ContextVersions, actual
			}
			phase2Atoms(t, input, gold.Cases[2].Required)
			if !phase2RefPresent(refs, ref) {
				t.Error("supplied confirmed claim lost exact typed dependency")
			}
			phase2Evidence(t, "dependencies", refs)
		})
	}
}

func TestPhase2NaturalSourceAuthorizationEntry(t *testing.T) {
	s, scope := phase2Store(t), owner()
	capture := phase2Model(t, s)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	record := phase2ReadGold(t).Records[0]
	source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-synthetic", ExternalID: record.ID, ExternalVersion: record.ExternalVersion, Title: "成都预约资料", Text: record.Text, MediaType: "text/plain"})
	// No SQL grant: this is the product authorization entry, measured separately.
	out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: "让秘书能用《成都预约资料》"})
	if err != nil {
		t.Fatal(err)
	}
	phase2Evidence(t, "natural-authorization-response", out.Turn)
	_ = capture.last(t)
	result, err := s.GetSource(context.Background(), memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "phase2-model"}, source.ID, source.Version)
	phase2Evidence(t, "natural-authorization-read", map[string]any{"source": source.Ref, "error": func() string {
		if err != nil {
			return err.Error()
		}
		return ""
	}(), "result": result})
	if err != nil {
		t.Errorf("explicit owner natural source authorization did not make source readable: %v", err)
	}
}

func TestPhase2ClaimGrantDoesNotAuthorizeSource(t *testing.T) {
	s, scope := phase2Store(t), owner()
	phase2Model(t, s)
	record := phase2ReadGold(t).Records[1]
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: record.Text})
	source := state.Candidates[0].Source
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: record.Claim})
	if len(state.Memories) != 1 {
		t.Fatal("positive claim fixture missing")
	}
	for _, agent := range []string{"phase2-model", "manual"} {
		_, err := s.GetSource(context.Background(), memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent}, memory.ID(source.SourceID), source.Version)
		phase2Evidence(t, "claim-only-source-read-"+agent, map[string]any{"agent": agent, "source": source, "error": func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}()})
		if err == nil {
			t.Errorf("claim grant expanded to original source for %s", agent)
		}
	}
}

func TestPhase2IndirectDependenciesNotActualInput(t *testing.T) {
	gold := phase2ReadGold(t)
	s, scope := phase2Store(t), owner()
	phase2Model(t, s)
	record := gold.Records[2]
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: record.Text})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: record.Claim})
	ref := memory.Ref{ID: memory.ID(state.Memories[0].ID), Version: state.Memories[0].Version, Kind: memory.ClaimKind}
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "独立依赖验收"})
	thing := state.Tasks[0].ID
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "manual", Kind: "ask", Prompt: "旧资料基底是什么？"})
	prior := state.Runs[0]
	phase2Atoms(t, prior.Brief, record.Atoms)
	if !phase2RefPresent(prior.ContextVersions, ref) {
		t.Fatal("prior run missing gold dependency")
	}
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: prior.ID, Output: record.DerivedOutput})
	// The large unrelated note forces input budget exclusion of current direct
	// claim text, while prior output remains legal and its dependency must survive.
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: thing, Text: strings.Repeat("普通任务材料。", 4000)})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "manual", Kind: "ask", Prompt: "沿用之前结果继续"})
	current := state.Runs[0]
	phase2Evidence(t, "indirect-run", current)
	if !strings.Contains(current.Brief, record.DerivedOutput) {
		t.Error("lawful prior derived output missing from actual manual package")
	}
	if !phase2RefPresent(current.ContextVersions, ref) {
		t.Error("indirect dependency lost when direct atom omitted")
	}
	for _, atom := range record.Atoms {
		if strings.Contains(current.Brief, atom) {
			t.Errorf("fixture failed to exclude direct source atom %q", atom)
		}
	}
	phase2Evidence(t, "four-set-diagnostic", map[string]any{"supplied_direct_atoms": []string{}, "supplied_derived_text": record.DerivedOutput, "indirect_dependencies": current.ContextVersions, "Used": "unknown for manual external output", "external_received": "unknown"})
}
