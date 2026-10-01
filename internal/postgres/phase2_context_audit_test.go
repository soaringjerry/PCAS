package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// This opt-in audit records current behavior; it does not define a new product
// contract. PCAS_PHASE2_AUDIT_REQUIRE_RAW=1 enables the deliberately failing
// future expectation on the audited baseline, without changing default CI.
func TestPhase2ContextAudit(t *testing.T) {
	if os.Getenv("PCAS_PHASE2_CONTEXT_AUDIT") != "1" {
		t.Skip("opt-in audit: set PCAS_PHASE2_CONTEXT_AUDIT=1 and a disposable PCAS_TEST_DATABASE_URL")
	}
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	var mu sync.Mutex
	requests := []string{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var request struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		parts := []string{}
		for _, message := range request.Messages {
			parts = append(parts, message.Role+": "+message.Content)
		}
		mu.Lock()
		requests = append(requests, strings.Join(parts, "\n"))
		mu.Unlock()
		secretaryModelReply(w, `{"reply":"审计响应","answer":"审计响应","used":[],"links":[],"actions":[],"remember":false,"ask":null}`)
	}))
	server.Listener.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:18150")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "审计假模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil { // Register agents before ingest/accept.
		t.Fatal(err)
	}
	lastRequest := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(requests) == 0 {
			t.Fatal("real provider request was not received")
		}
		return requests[len(requests)-1]
	}
	t.Run("authorized_unstructured_source", func(t *testing.T) {
		const query = "auditroute 交付暗号是什么"
		const marker = "rawSecretM1"
		in := memory.IngestRequest{Connector: "audit", ExternalID: "raw-route", ExternalVersion: "1", Title: "未结构化资料", Text: "auditroute 的交付暗号是 rawSecretM1。", MediaType: "text/plain"}
		source := mustIngest(t, s, scope, in).Ref
		var grants int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM record_grants WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(source.ID)).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("source ingest authorization observation changed: grants=%d err=%v", grants, err)
		}
		t.Log("public Ingest after agent registration: source grants=0; following source grants are explicit audit fixture setup")
		// There is no public source-grant mutation API. Grant only this synthetic
		// fixture explicitly; all retrieval and generation use public entrypoints.
		if _, err := s.pool.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,'model'),($1,$2,'manual')", string(scope.OwnerID), string(source.ID)); err != nil {
			t.Fatal(err)
		}
		var claims int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&claims); err != nil || claims != 0 {
			t.Fatalf("requires zero claims: %d, %v", claims, err)
		}
		recall, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, memory.RecallRequest{Query: query, Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}})
		if err != nil || !strings.Contains(recall.Summary, marker) {
			t.Fatalf("authorized source not recalled: %+v, %v", recall, err)
		}
		found := false
		for _, ref := range recall.Memories {
			found = found || ref == source
		}
		if !found {
			t.Fatal("source ref absent from retrieval candidates")
		}
		t.Logf("agent Recall: source=%+v Summary=%q coverage=%+v", source, recall.Summary, recall.Coverage)
		// Mirror live_replay's direct-summary generation solely to establish the
		// difference from the actual app assembly for the same authorized fixture.
		if _, err := s.models.Generate(ctx, "model", "审计检索基线", string(asJSON(map[string]any{"query": query, "context": recall.Summary, "gaps": recall.Coverage.Gaps}))); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(lastRequest(), marker) {
			t.Fatal("direct-summary provider did not receive source text")
		}
		out := mustTurn(t, s, scope, turnRequest(query))
		if out.Turn.Reply != "审计响应" {
			t.Fatal("DeskTurn fell back instead of invoking the model", out.Turn)
		}
		prompt := lastRequest()
		var dependencies []memory.Ref
		if err := s.pool.QueryRow(ctx, "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), out.Turn.ID).Scan(&dependencies); err != nil {
			t.Fatal(err)
		}
		t.Logf("DeskTurn provider raw_present=%t dependencies=%+v request=%q", strings.Contains(prompt, marker), dependencies, prompt)
		if os.Getenv("PCAS_PHASE2_AUDIT_REQUIRE_RAW") == "1" && !strings.Contains(prompt, marker) {
			t.Error("future expectation: authorized raw Recall hit was dropped before DeskTurn provider request")
		}
		state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "审计装配"})
		thingID := state.Tasks[0].ID
		for _, agent := range []string{"manual", "model"} {
			state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thingID, AgentID: agent, Kind: "ask", Prompt: query})
			var run workspace.Run
			for _, candidate := range state.Runs {
				if candidate.AgentID == agent {
					run = candidate
					break
				}
			}
			if run.ID == "" {
				t.Fatal("run missing")
			}
			t.Logf("requestRun agent=%s raw_present=%t dependencies=%+v brief=%q", agent, strings.Contains(run.Brief, marker), run.ContextVersions, run.Brief)
			if agent == "model" {
				jobCtx, cancel := context.WithCancel(ctx)
				done := make(chan error, 1)
				go func() { done <- s.RunAgents(jobCtx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					current, err := s.Snapshot(ctx, scope)
					if err != nil {
						cancel()
						t.Fatal(err)
					}
					for _, result := range current.Runs {
						if result.ID == run.ID && result.Status == "done" {
							cancel()
							if err := <-done; err != nil {
								t.Fatal(err)
							}
							t.Logf("RunAgents provider raw_present=%t request=%q", strings.Contains(lastRequest(), marker), lastRequest())
							return
						}
					}
					time.Sleep(20 * time.Millisecond)
				}
				cancel()
				<-done
				t.Fatal("RunAgents did not finish the synthetic run")
			}
		}
	})
	t.Run("confirmed_claim_next_turn_correction_delete", func(t *testing.T) {
		const query = "auditclaim 交付暗号是什么"
		state := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "auditclaim 的交付暗号是 claimBeforeM1。"})
		state = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "auditclaim 的交付暗号是 claimBeforeM1。"})
		claimID := state.Memories[0].ID
		before := mustTurn(t, s, scope, turnRequest(query))
		if before.Turn.Reply != "审计响应" || !strings.Contains(lastRequest(), "claimBeforeM1") {
			t.Fatal("confirmed claim not supplied to actual model")
		}
		// Used is deliberately empty: input dependencies must still survive.
		var refs []memory.Ref
		if err := s.pool.QueryRow(ctx, "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), before.Turn.ID).Scan(&refs); err != nil || len(refs) != 1 || string(refs[0].ID) != claimID {
			t.Fatalf("input dependency not recorded independently of Used: %+v %v", refs, err)
		}
		workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: claimID, Text: "auditclaim 的交付暗号是 claimAfterM1。", Reason: "审计纠正"})
		mustTurn(t, s, scope, turnRequest(query))
		if !strings.Contains(lastRequest(), "claimAfterM1") || strings.Contains(lastRequest(), "claimBeforeM1") {
			t.Fatal("next turn did not use corrected claim")
		}
		t.Logf("confirmed claim next turn: correction supplies claimAfterM1, excludes claimBeforeM1; dependencies recorded despite Used=[]: %+v", refs)
		workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: claimID, IncludeSources: true})
		mustTurn(t, s, scope, turnRequest(query))
		if strings.Contains(lastRequest(), "claimAfterM1") || strings.Contains(lastRequest(), "claimBeforeM1") {
			t.Fatal("deleted text supplied to next actual model request")
		}
		t.Log("confirmed claim next turn: deleteMemory(includeSources=true) excludes both claim markers")
	})
}
