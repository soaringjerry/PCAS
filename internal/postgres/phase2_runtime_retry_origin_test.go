package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type phase2RTRetryFixture struct {
	s                 *Store
	scope             memory.Scope
	capture           *phase2RTCapture
	source            memory.IngestResult
	secretary, deputy phase2RTAuthResult
	hard              memory.HardScope
	studio            string
	old               workspace.Run
	origin            memory.ID
}

// A real loopback provider sends 503 on the first deputy call. The inner
// existing capture receives identical request bytes, retaining its schema-aware
// successful reply behavior for subsequent calls; no failed DB state is forged.
func phase2RTRetryFailFirstDeputy(t *testing.T, c *phase2RTCapture) {
	t.Helper()
	original := c.Registry.Config.Providers[0].BaseURL
	client := c.Registry.HTTP
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "synthetic read failed", 500)
			return
		}
		next, err := http.NewRequestWithContext(r.Context(), r.Method, original+r.URL.RequestURI(), bytes.NewReader(raw))
		if err != nil {
			t.Error(err)
			return
		}
		next.Header = r.Header.Clone()
		response, err := client.Do(next)
		if err != nil {
			t.Error(err)
			http.Error(w, "synthetic forwarding failed", 500)
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Error(err)
			return
		}
		n := calls.Add(1)
		if n == 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			failedBody := []byte(`{"error":{"message":"synthetic retry 503"}}`)
			phase2RTEvidence(t, "retry-provider-wire-"+string(memory.NewID()), map[string]any{"ordinal": n, "request": json.RawMessage(raw), "status": http.StatusServiceUnavailable, "response": json.RawMessage(failedBody)})
			_, _ = w.Write(failedBody)
			return
		}
		for k, vs := range response.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(response.StatusCode)
		phase2RTEvidence(t, "retry-provider-wire-"+string(memory.NewID()), map[string]any{"ordinal": n, "request": json.RawMessage(raw), "status": response.StatusCode, "response": json.RawMessage(body)})
		_, _ = w.Write(body)
	}))
	t.Cleanup(proxy.Close)
	c.Registry.Config.Providers[0].BaseURL = proxy.URL
	c.Registry.HTTP = proxy.Client()
}

func phase2RTRetrySetup(t *testing.T, studio, ownerPrompt bool) *phase2RTRetryFixture {
	t.Helper()
	s, scope, c := phase2RTSetup(t)
	phase2RTRetryFailFirstDeputy(t, c)
	f := &phase2RTRetryFixture{s: s, scope: scope, capture: c, source: phase2RTSource(t, s, scope), hard: phase2RTUnscoped()}
	if studio {
		f.studio = string(memory.NewID())
		workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", ID: f.studio, Name: "Retry studio A"})
		f.hard = memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID(f.studio), IncludeGlobalConstraints: true}
		phase2RTScopeSet(t, s, scope, f.source.Ref, 0, []memory.SourceScopeAssignment{{Kind: memory.StudioSourceScope, StudioID: memory.ID(f.studio)}})
	}
	f.secretary, _ = phase2RTAuthorize(t, s, scope, f.source.Ref, "phase2-model", "secretary", f.hard)
	f.deputy, _ = phase2RTAuthorize(t, s, scope, f.source.Ref, "phase2-model", "deputy", f.hard)
	gold := phase2RTGoldRead(t)
	if ownerPrompt {
		item, origin := phase2RTNameCreate(t, s, scope, c, f.source.Ref, "task")
		f.origin = origin
		id := string(memory.NewID())
		state := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ID: id, ThingID: item.ID, AgentID: "phase2-model", Kind: "ask", Prompt: "RETRY-OWNER-INDEPENDENT-865 给普通工作建议"})
		f.old = phase2RTRetryRun(t, state, id)
		if len(f.old.ContextDeskActions) == 0 || len(phase2RTPromptOriginIDs(t, f.old)) != 0 {
			t.Fatal("owner Prompt control must have inherited general origins but no Prompt origins", f.old)
		}
	} else {
		actions := []any{map[string]any{"op": "delegate", "ref": "new", "title": "Retry source task " + gold.Records[0].Atoms[0], "prompt": "RETRY-GENERATED-PROMPT-864 整理成都预约资料，预约码 " + gold.Records[0].Atoms[0], "kind": "draft"}}
		if studio {
			actions = []any{map[string]any{"op": "create_task", "project": "THIS", "title": "Retry source task " + gold.Records[0].Atoms[0]}, map[string]any{"op": "delegate", "ref": "N1", "prompt": "RETRY-GENERATED-PROMPT-864 整理成都预约资料，预约码 " + gold.Records[0].Atoms[0], "kind": "draft"}}
		}
		c.mu.Lock()
		c.Reply = string(asJSON(map[string]any{"reply": "RETRY-SECRETARY-863", "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": actions}))
		c.mu.Unlock()
		request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: gold.Cases[0].Query + "请让副手整理资料"}
		if studio {
			request.ThingID = &f.studio
		}
		out, err := s.DeskTurn(context.Background(), scope, request)
		if err != nil || c.count() != 1 {
			t.Fatal("retry real secretary/delegation positive control missing", err, c.count(), out)
		}
		phase2RTEvidence(t, "retry-real-secretary-proposal", map[string]any{"request": request, "actual_http": json.RawMessage(c.request(t, 0)), "proposal": actions, "response": out})
		f.old = phase2RTDelegatedRun(t, out)
		if studio {
			var wire struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(c.request(t, 0), &wire); err != nil {
				t.Fatal(err)
			}
			actualAlias := false
			for _, m := range wire.Messages {
				actualAlias = actualAlias || strings.Contains(m.Content, "THIS：\nRetry studio A")
			}
			if !actualAlias || request.ThingID == nil || *request.ThingID != f.studio {
				t.Fatal("studio THIS alias was not actually supplied from the real project", wire)
			}
			if len(out.Turn.Receipts) != 2 || out.Turn.Receipts[0].Op != "create_task" || out.Turn.Receipts[0].Status != "done" || out.Turn.Receipts[0].ActionID == nil || out.Turn.Receipts[0].ThingID == nil || out.Turn.Receipts[1].Op != "delegate" || out.Turn.Receipts[1].Status != "done" || out.Turn.Receipts[1].ActionID == nil {
				t.Fatal("studio actual two-action positive control missing", out.Turn.Receipts)
			}
			task := phase2RTNameItem(t, out.State, *out.Turn.Receipts[0].ThingID)
			if task.Kind != "task" || task.ProjectID != f.studio || f.old.ThingID != task.ID {
				t.Fatal("N1 must be the real task in A, not project itself", task, f.old)
			}
			createID := memory.ID(*out.Turn.Receipts[0].ActionID)
			delegateID := memory.ID(*out.Turn.Receipts[1].ActionID)
			origins := phase2RTDeskActionIDs(t, f.old)
			if len(origins) != 2 || !phase2RTRetryContainsOrigin(origins, createID) || !phase2RTRetryContainsOrigin(origins, delegateID) {
				t.Fatal("studio general closure lost true create/delegate identities", origins, out.Turn.Receipts)
			}
		}
		phase2RTRequireDelegatePromptOrigin(t, f.old, out)
		f.origin = phase2RTPromptOriginIDs(t, f.old)[0]
		secretaryAttempt := phase2RTSnapshotForSource(t, s, scope, f.source.Ref, "secretary", request.RequestID, c.request(t, 0))
		if secretaryAttempt.Manifest.Scope != f.hard {
			t.Fatal("actual secretary scope differs from studio policy", secretaryAttempt.Manifest.Scope, f.hard)
		}
	}
	if f.old.ContextTask == nil || f.old.ContextTask.Scope != f.hard {
		t.Fatal("retry original current scope missing", f.old.ContextTask, f.hard)
	}
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.old = phase2RTWaitRun(t, s, scope, f.old.ID)
	if f.old.Status != "failed" || c.count() != 2 || f.old.Output != "" {
		t.Fatal("original deputy must truly fail after exactly one 503 HTTP", f.old, c.count())
	}
	oldAttempt := phase2RTSnapshotForSource(t, s, scope, f.source.Ref, "deputy", f.old.ID, c.request(t, 1))
	phase2RTEvidence(t, "retry-original-failed-run", map[string]any{"run": f.old, "origin": f.origin, "source": f.source.Ref, "attempt": oldAttempt, "http": json.RawMessage(c.request(t, 1)), "provider_status": 503})
	return f
}

func phase2RTRetryRun(t *testing.T, state workspace.State, id string) workspace.Run {
	t.Helper()
	for _, r := range state.Runs {
		if r.ID == id {
			return r
		}
	}
	t.Fatal("formal requestRun omitted actual new Run", id)
	return workspace.Run{}
}
func (f *phase2RTRetryFixture) command(t *testing.T, scope memory.Scope, thing, agent, prompt string) workspace.Command {
	t.Helper()
	st, err := f.s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	c := workspace.Command{Type: "requestRun", ID: string(memory.NewID()), RequestID: string(memory.NewID()), ExpectedRevision: st.Revision, ThingID: thing, AgentID: agent, Kind: "ask", Prompt: prompt, SourceRunID: f.old.ID}
	if agent == "manual" {
		c.ManualRecipient = &memory.Recipient{Provider: "phase2-model"}
	}
	return c
}
func (f *phase2RTRetryFixture) setAnswer() {
	f.capture.mu.Lock()
	f.capture.Reply = string(asJSON(map[string]any{"reply": "RETRY-NEW-RESULT-867", "used": []any{}}))
	f.capture.mu.Unlock()
}
func (f *phase2RTRetryFixture) extraTarget(t *testing.T, allow bool) phase2RTAuthResult {
	t.Helper()
	p := f.capture.Registry.Config.Providers[0]
	p.ID = "phase2-retry-target"
	p.Name = "Retry independent target"
	p.Model = "phase2-retry-other-model"
	f.capture.Registry.Config.Providers = append(f.capture.Registry.Config.Providers, p)
	if _, err := f.s.Snapshot(context.Background(), f.scope); err != nil {
		t.Fatal(err)
	}
	if allow {
		g, _ := phase2RTAuthorize(t, f.s, f.scope, f.source.Ref, p.ID, "deputy", f.hard)
		return g
	}
	return phase2RTAuthResult{}
}
func (f *phase2RTRetryFixture) negative(t *testing.T, scope memory.Scope, c workspace.Command, want error) {
	t.Helper()
	before := f.capture.count()
	st, err := f.s.Execute(context.Background(), scope, c)
	phase2RTEvidence(t, "retry-refused", map[string]any{"command": c, "state": st, "error": phase2RTRetryError(err), "want_error": phase2RTRetryError(want), "http_delta": f.capture.count() - before})
	if !errors.Is(err, want) {
		t.Fatalf("retry expected exact controlled %v, got %v", want, err)
	}
	if f.capture.count() != before {
		t.Fatal("invalid retry caused provider dispatch")
	}
	var committed int
	if err := f.s.pool.QueryRow(context.Background(), "SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID).Scan(&committed); err != nil || committed != 0 {
		t.Fatal("rejected retry committed a new Run", err, committed)
	}
	attempts, err := f.s.ContextAttempts(context.Background(), scope, c.ID)
	if err != nil || len(attempts) != 0 {
		t.Fatal("rejected retry created/delivered/reused a context attempt", err, attempts)
	}
	phase2RTEvidence(t, "retry-no-new-run-or-attempt", map[string]any{"new_run_id": c.ID, "new_run_rows": committed, "context_attempts": attempts, "http_delta": f.capture.count() - before})
}
func phase2RTRetryError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (f *phase2RTRetryFixture) success(t *testing.T, c workspace.Command, grant phase2RTAuthResult, independent bool) {
	t.Helper()
	f.setAnswer()
	before := f.capture.count()
	state, err := f.s.Execute(context.Background(), f.scope, c)
	if err != nil {
		t.Fatal(err)
	}
	queued := phase2RTRetryRun(t, state, c.ID)
	if queued.ID == f.old.ID || queued.ContextTask == nil || !queued.ContextTask.Now.After(f.old.ContextTask.Now) {
		t.Fatal("retry reused original Run/current Task", queued)
	}
	origins := phase2RTPromptOriginIDs(t, queued)
	if independent {
		if len(origins) != 0 {
			t.Fatal("independent owner Prompt acquired generated origins", origins)
		}
	} else if len(origins) != 1 || origins[0] != f.origin {
		t.Fatal("retry lost/substituted actual delegate Prompt origin", origins, f.origin)
	}
	if err := f.s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	finished := phase2RTWaitRun(t, f.s, f.scope, c.ID)
	if finished.Status != "done" || finished.Output != "RETRY-NEW-RESULT-867" || f.capture.count() != before+1 {
		t.Fatal("retry did not actually finish one new provider call", finished, f.capture.count()-before)
	}
	payload := f.capture.request(t, before)
	if !strings.Contains(string(payload), c.Prompt) {
		t.Error("actual retry HTTP lost edited owner/generated prompt")
	}
	var attempt memory.ContextAttempt
	if independent {
		attempts, err := f.s.ContextAttempts(context.Background(), f.scope, c.ID)
		if err != nil || len(attempts) != 1 {
			t.Fatal("new independent retry attempt missing", err, attempts)
		}
		attempt = attempts[0]
		snapshot, err := f.s.ContextAttemptSnapshot(context.Background(), f.scope, attempt.ID)
		if err != nil || !bytes.Equal(snapshot, payload) {
			t.Fatal("independent retry final bytes differ", err)
		}
		if len(attempt.Manifest.DeskActions) != 0 || len(finished.ContextDeskActions) != 0 {
			t.Error("new independent owner retry borrowed invalid old general action context", finished.ContextDeskActions, attempt.Manifest.DeskActions)
		}
	} else {
		attempt = phase2RTSnapshotForSource(t, f.s, f.scope, f.source.Ref, "deputy", c.ID, payload)
	}
	if attempt.ID == f.old.ContextAttemptID || attempt.Manifest.Recipient != grant.Authorization.Recipient {
		t.Error("retry reused old attempt/destination", attempt.ID, attempt.Manifest.Recipient)
	}
	if !independent {
		phase2RTDelegationLineage(t, attempt, f.source.Ref, grant)
		expectedOrigins := map[memory.ID]bool{}
		for _, id := range f.old.ContextDeskActions {
			if expectedOrigins[id] {
				t.Fatal("original general action control has duplicate origins", id)
			}
			expectedOrigins[id] = true
		}
		actualOrigins := map[memory.ID]bool{}
		if len(attempt.Manifest.DeskActions) != len(f.old.ContextDeskActions) {
			t.Error("new manifest general origins are not the exact original actual set", attempt.Manifest.DeskActions, f.old.ContextDeskActions)
		}
		for _, id := range attempt.Manifest.DeskActions {
			if actualOrigins[id] || !expectedOrigins[id] {
				t.Error("new manifest contains duplicate or extra general action origin", id)
			}
			actualOrigins[id] = true
		}
		for id := range expectedOrigins {
			if !actualOrigins[id] {
				t.Error("new manifest lost actual original general action origin", id)
			}
		}
	}
	if len(attempt.Manifest.Used) != 0 {
		t.Error("fake Used=[] should remain separately empty", attempt.Manifest.Used)
	}
	phase2RTEvidence(t, "retry-success", map[string]any{"command": c, "old": f.old, "new": finished, "attempt": attempt, "actual_http": json.RawMessage(payload)})
}

func TestPhase2RuntimeRetryPreservesGeneratedPromptOrigins(t *testing.T) {
	var gold struct {
		Cases []struct {
			ID string `json:"id"`
		}
	}
	phase2RTReadJSON(t, "retry-origin-sequences.json", &gold)
	if len(gold.Cases) != 13 {
		t.Fatal("original thirteen retry controls missing")
	}
	for _, tc := range gold.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			f := phase2RTRetrySetup(t, tc.ID == "RETRY-LIVE" || tc.ID == "RETRY-SCOPE-DENIED", tc.ID == "RETRY-OWNER-INDEPENDENT")
			embedding := phase2RTRetryEmbeddingTrap(t, f)
			if tc.ID != "RETRY-OWNER-INDEPENDENT" {
				t.Cleanup(func() {
					if embedding.count() != 0 {
						t.Error("generated retry query reached unauthorized embedding recipient", embedding.count())
					}
				})
			}
			prompt := f.old.Prompt
			if strings.HasSuffix(tc.ID, "PUNCTUATION_ONLY") || tc.ID == "RETRY-NEW-RECIPIENT" {
				prompt += "。"
			}
			agent := "phase2-model"
			switch tc.ID {
			case "RETRY-LIVE":
				f.success(t, f.command(t, f.scope, f.old.ThingID, agent, prompt), f.deputy, false)
			case "RETRY-NEW-RECIPIENT":
				g := f.extraTarget(t, true)
				f.success(t, f.command(t, f.scope, f.old.ThingID, "phase2-retry-target", prompt), g, false)
			case "RETRY-TARGET-DENIED":
				f.extraTarget(t, false)
				f.negative(t, f.scope, f.command(t, f.scope, f.old.ThingID, "phase2-retry-target", prompt), memory.ErrForbidden)
			case "RETRY-SCOPE-DENIED":
				b := string(memory.NewID())
				workspaceCommand(t, f.s, f.scope, workspace.Command{Type: "addProject", ID: b, Name: "Retry studio B"})
				hard := f.hard
				hard.StudioID = memory.ID(b)
				bPolicy, _ := phase2RTAuthorize(t, f.s, f.scope, f.source.Ref, agent, "deputy", hard)
				assignments, err := f.s.SourceScope(context.Background(), f.scope, f.source.ID)
				if err != nil || len(assignments.Assignments) != 1 || assignments.Assignments[0].StudioID != memory.ID(f.studio) || bPolicy.Authorization.Revoked || bPolicy.Authorization.Scope != hard {
					t.Fatal("B denial must isolate actual source assignment from independent canonical B allow", err, assignments, bPolicy)
				}
				workspaceCommand(t, f.s, f.scope, workspace.Command{Type: "moveThing", ID: f.old.ThingID, ProjectID: b})
				f.negative(t, f.scope, f.command(t, f.scope, f.old.ThingID, agent, prompt), memory.ErrForbidden)
			case "RETRY-OWNER-INDEPENDENT":
				phase2RTUpdatePolicy(t, f.s, f.scope, f.source.Ref, f.secretary, true)
				var original workspace.Run
				if err := f.s.pool.QueryRow(context.Background(), "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), f.old.ID).Scan(&original); err != nil || !original.StaleContext || original.Prompt != prompt {
					t.Fatal("owner old general-context stale positive control missing", err, original)
				}
				f.success(t, f.command(t, f.scope, f.old.ThingID, agent, prompt), f.deputy, true)
				phase2RTRetryEmbeddingOnly(t, embedding, prompt)
			case "RETRY-LOCATOR-CROSS_OWNER", "RETRY-LOCATOR-CROSS_THING":
				scope := f.scope
				want := memory.ErrForbidden
				if strings.HasSuffix(tc.ID, "CROSS_OWNER") {
					scope = owner()
					want = memory.ErrNotFound
				}
				id := string(memory.NewID())
				workspaceCommand(t, f.s, scope, workspace.Command{Type: "addTask", ID: id, Title: "Retry distinct item"})
				f.negative(t, scope, f.command(t, scope, id, agent, prompt), want)
			default:
				switch {
				case strings.Contains(tc.ID, "REVOKE_SECRETARY_ONLY"):
					phase2RTUpdatePolicy(t, f.s, f.scope, f.source.Ref, f.secretary, true)
				case strings.Contains(tc.ID, "CORRECT_SOURCE_TO_V2"):
					var g phase2RTLifecycleGold
					phase2RTReadJSON(t, "runtime-sequences.json", &g)
					if err := phase2RTMutation(context.Background(), f.s, f.scope, f.source.Ref, f.secretary, "replace", g); err != nil {
						t.Fatal(err)
					}
				case strings.Contains(tc.ID, "DELETE_SOURCE"):
					phase2RTRetryDeleteLocator(t, f)
				default:
					t.Fatal("unknown frozen retry case", tc.ID)
				}
				want := memory.ErrConflict
				if strings.Contains(tc.ID, "DELETE_SOURCE") {
					want = memory.ErrNotFound
				}
				f.negative(t, f.scope, f.command(t, f.scope, f.old.ThingID, agent, prompt), want)
			}
		})
	}
}

func TestPhase2RuntimeRetryManualAndFinalFence(t *testing.T) {
	for _, name := range []string{"RETRY-MANUAL-LIVE", "RETRY-MANUAL-REVOKED", "RETRY-FINAL-BARRIER"} {
		t.Run(name, func(t *testing.T) {
			f := phase2RTRetrySetup(t, false, false)
			embedding := phase2RTRetryEmbeddingTrap(t, f)
			t.Cleanup(func() {
				if embedding.count() != 0 {
					t.Error("generated manual/final retry reached embedding recipient", embedding.count())
				}
			})
			if name == "RETRY-FINAL-BARRIER" {
				phase2RTRetryFinalBarrier(t, f)
				return
			}
			manual, _ := phase2RTAuthorize(t, f.s, f.scope, f.source.Ref, "manual", "manual", f.hard)
			command := f.command(t, f.scope, f.old.ThingID, "manual", f.old.Prompt+"。")
			if name == "RETRY-MANUAL-REVOKED" {
				phase2RTUpdatePolicy(t, f.s, f.scope, f.source.Ref, f.secretary, true)
				f.negative(t, f.scope, command, memory.ErrConflict)
				w := phase2RTHTTP(t, f.s, f.scope, http.MethodGet, "/v1/workspace/runs/"+command.ID+"/package", nil)
				if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), phase2RTGoldRead(t).Records[0].Atoms[0]) {
					t.Fatal("rejected manual retry delivered a package", w.Code, w.Body.String())
				}
				return
			}
			state, err := f.s.Execute(context.Background(), f.scope, command)
			if err != nil {
				t.Fatal(err)
			}
			run := phase2RTRetryRun(t, state, command.ID)
			ids := phase2RTPromptOriginIDs(t, run)
			if len(ids) != 1 || ids[0] != f.origin || run.ContextTask == nil || run.ContextTask.Recipient != manual.Authorization.Recipient {
				t.Fatal("manual retry lost server origin/current target", run)
			}
			before := f.capture.count()
			pkg := phase2RTGetPackage(t, f.s, f.scope, run)
			if f.capture.count() != before || !strings.Contains(pkg.Text, command.Prompt) {
				t.Fatal("manual delivery lost edited prompt or invoked automatic provider")
			}
			phase2RTAtoms(t, pkg.Text, phase2RTGoldRead(t).Cases[0].Required, true)
			attempt := phase2RTSnapshotForSource(t, f.s, f.scope, f.source.Ref, "manual", run.ID, []byte(pkg.Text))
			phase2RTDelegationLineage(t, attempt, f.source.Ref, manual)
			if attempt.DeliveredAt == nil || attempt.ExternalReceipt != "unknown" || attempt.ID == f.old.ContextAttemptID {
				t.Error("manual new delivery reused old attempt or fabricated external receipt", attempt)
			}
		})
	}
}

// The original external-embedding barrier design is preserved in the first
// harness commit/gold. The current contract forbids that generated query from
// reaching embedding; a real owner-row wait proves prepare->final ordering.
func phase2RTRetryFinalBarrier(t *testing.T, f *phase2RTRetryFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	locker, err := pgx.ConnectConfig(ctx, f.s.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close(context.Background())
	monitor, err := pgx.ConnectConfig(ctx, f.s.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close(context.Background())
	fixture, err := locker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback(context.Background())
	var revision int64
	if err := fixture.QueryRow(ctx, "SELECT revision FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(f.scope.OwnerID)).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	lockedAt := time.Now().UTC()
	lockerPID := int32(locker.PgConn().PID())
	// The formal policy mutation enters the actual owner gate before retry.
	type revokeResult struct {
		receipt memory.SourceAuthorizationResult
		err     error
	}
	revoked := make(chan revokeResult, 1)
	in := memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: f.source.Ref, PolicyID: f.secretary.Authorization.ID, ExpectedPolicyRevision: f.secretary.Authorization.Revision, Recipient: f.secretary.Authorization.Recipient, Purpose: f.secretary.Authorization.Purpose, Scope: f.secretary.Authorization.Scope, Revoke: true}
	go func() {
		receipt, err := f.s.SetSourceAuthorization(ctx, f.scope, in)
		revoked <- revokeResult{receipt, err}
	}()
	wait := func(blocker int32) phase2RTRetryWait {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			rows, err := monitor.Query(ctx, "SELECT pid,wait_event_type,wait_event,query,pg_blocking_pids(pid) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))", blocker)
			if err != nil {
				t.Fatal(err)
			}
			var found *phase2RTRetryWait
			for rows.Next() {
				var w phase2RTRetryWait
				if err := rows.Scan(&w.PID, &w.WaitType, &w.WaitEvent, &w.Query, &w.Blockers); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if strings.Contains(w.Query, "SELECT revision FROM workspace_owners WHERE owner_id=$1 FOR UPDATE") {
					w.ObservedAt = time.Now().UTC()
					found = &w
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if found != nil {
				return *found
			}
			select {
			case <-ctx.Done():
				t.Fatal("real owner-row final wait not observed; no assumed prepared state", blocker)
			case <-ticker.C:
			}
		}
	}
	revokeWait := wait(lockerPID)
	command := workspace.Command{Type: "requestRun", ID: string(memory.NewID()), RequestID: string(memory.NewID()), ExpectedRevision: revision, ThingID: f.old.ThingID, AgentID: "phase2-model", Kind: "ask", Prompt: f.old.Prompt + "。", SourceRunID: f.old.ID}
	type retryResult struct {
		state workspace.State
		err   error
	}
	retried := make(chan retryResult, 1)
	go func() { st, err := f.s.Execute(ctx, f.scope, command); retried <- retryResult{st, err} }()
	retryWait := wait(revokeWait.PID)
	if retryWait.PID == revokeWait.PID || !retryWait.ObservedAt.After(revokeWait.ObservedAt) {
		t.Fatal("two true prepare/final and revoke waiters were not independently observed", revokeWait, retryWait)
	}
	releasedAt := time.Now().UTC()
	if err := fixture.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var mutation revokeResult
	select {
	case mutation = <-revoked:
	case <-ctx.Done():
		t.Fatal("queued formal revoke did not commit")
	}
	returnedAt := time.Now().UTC()
	if mutation.err != nil || !mutation.receipt.Authorization.Revoked || mutation.receipt.Authorization.Revision != f.secretary.Authorization.Revision+1 {
		t.Fatal("queued formal source mutation failed", mutation)
	}
	policies, err := f.s.SourceAuthorizations(ctx, f.scope, f.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	persisted := false
	for _, policy := range policies {
		if policy.ID == f.secretary.Authorization.ID {
			persisted = policy.Revoked && policy.ExplicitDeny && policy.Revision == mutation.receipt.Authorization.Revision
		}
	}
	if !persisted {
		t.Fatal("formal revoke returned without persistent expected deny")
	}
	var result retryResult
	select {
	case result = <-retried:
	case <-ctx.Done():
		t.Fatal("queued final retry did not return")
	}
	if !errors.Is(result.err, memory.ErrConflict) || f.capture.count() != 2 {
		t.Fatal("actual queued final fence accepted revoked generated Prompt", result.err, f.capture.count())
	}
	var count int
	if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), command.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked final retry committed a new Run", err, count)
	}
	phase2RTEvidence(t, "retry-final-owner-row-gate", map[string]any{"command": command, "fixture_pid": lockerPID, "locked_at": lockedAt, "revoke_wait": revokeWait, "retry_final_wait": retryWait, "fixture_release_at": releasedAt, "formal_revoke_returned_at": returnedAt, "formal_revoke_receipt": mutation.receipt, "persistent_source_policies": policies, "error": phase2RTRetryError(result.err), "new_runs": count, "new_deputy_http": f.capture.count() - 2})
}

type phase2RTRetryWait struct {
	PID                        int32
	WaitType, WaitEvent, Query string
	Blockers                   []int32
	ObservedAt                 time.Time
}

type phase2RTRetryEmbedding struct {
	mu       sync.Mutex
	requests [][]byte
}

func (e *phase2RTRetryEmbedding) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}
func phase2RTRetryEmbeddingTrap(t *testing.T, f *phase2RTRetryFixture) *phase2RTRetryEmbedding {
	t.Helper()
	e := &phase2RTRetryEmbedding{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		e.mu.Lock()
		e.requests = append(e.requests, append([]byte(nil), raw...))
		e.mu.Unlock()
		phase2RTEvidence(t, "retry-embedding-wire-"+string(memory.NewID()), map[string]any{"actual_payload": json.RawMessage(raw)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	f.capture.Registry.Config.Embedding = "phase2-retry-embedding"
	f.capture.Registry.Config.Providers = append(f.capture.Registry.Config.Providers, ai.Provider{ID: "phase2-retry-embedding", Protocol: "openai", BaseURL: server.URL, Model: "synthetic-vector", Embedding: true, CostMode: "free"})
	return e
}
func phase2RTRetryEmbeddingOnly(t *testing.T, e *phase2RTRetryEmbedding, want string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) != 1 {
		t.Fatal("ordinary current owner vector query was disabled or duplicated", len(e.requests))
	}
	var payload struct {
		Input []string `json:"input"`
	}
	if err := json.Unmarshal(e.requests[0], &payload); err != nil || len(payload.Input) != 1 || payload.Input[0] != want {
		t.Fatal("embedding recipient received anything beyond exact current owner text", err, string(e.requests[0]), want)
	}
}

func TestPhase2RuntimeRetryQueryIsolationAndDestinationExclusion(t *testing.T) {
	for _, name := range []string{"RETRY-TARGET-EXCLUDED", "SECRETARY-EMBED-CURRENT-QUERY-ONLY"} {
		t.Run(name, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			if name == "SECRETARY-EMBED-CURRENT-QUERY-ONLY" {
				source := phase2RTSource(t, s, scope)
				phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
				capture.mu.Lock()
				capture.Reply = string(asJSON(map[string]any{"reply": "EMBED-PRIOR-ANSWER-869 " + phase2RTGoldRead(t).Records[0].Atoms[0], "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": []any{}}))
				capture.mu.Unlock()
				firstRequest := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query}
				first, err := s.DeskTurn(context.Background(), scope, firstRequest)
				if err != nil || capture.count() != 1 {
					t.Fatal("actual prior secretary raw Input missing", err, capture.count())
				}
				phase2RTSnapshotForSource(t, s, scope, source.Ref, "secretary", firstRequest.RequestID, capture.request(t, 0))
				f := &phase2RTRetryFixture{s: s, scope: scope, capture: capture}
				embedding := phase2RTRetryEmbeddingTrap(t, f)
				const currentQuery = "请继续给普通建议 EMBED-OWNER-NOW-870"
				next := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), ConversationID: &first.ConversationID, AgentID: "phase2-model", Text: currentQuery}
				out, err := s.DeskTurn(context.Background(), scope, next)
				if err != nil || capture.count() != 2 {
					t.Fatal("ordinary secretary/vector follow-up failed", err, capture.count())
				}
				phase2RTRetryEmbeddingOnly(t, embedding, currentQuery)
				if !strings.Contains(string(capture.request(t, 1)), "EMBED-PRIOR-ANSWER-869") {
					t.Error("secretary positive history was lost while restricting embedding")
				}
				phase2RTEvidence(t, "secretary-current-only-embedding", map[string]any{"first": first, "current_request": next, "second": out, "actual_model_http": json.RawMessage(capture.request(t, 1)), "embedding_calls": embedding.count()})
				return
			}
			phase2RTRetryFailFirstDeputy(t, capture)
			_, claim := phase2RTClaimFixture(t, s, scope, "排除边界合成资料：约定代码 EXCLUDED-CLAIM-871。", "排除边界约定代码是 EXCLUDED-CLAIM-871。", "fact")
			capture.mu.Lock()
			capture.Reply = string(asJSON(map[string]any{"reply": "EXCLUDED-CLAIM-RECEIPT-872", "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": []any{map[string]any{"op": "delegate", "ref": "new", "title": "排除边界安排", "prompt": "整理排除边界约定代码 EXCLUDED-CLAIM-871", "kind": "draft"}}}))
			capture.mu.Unlock()
			request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: "排除边界约定代码是什么，请交副手整理"}
			original, err := s.DeskTurn(context.Background(), scope, request)
			if err != nil || capture.count() != 1 {
				t.Fatal("formal claim-derived delegation missing", err, original)
			}
			old := phase2RTDelegatedRun(t, original)
			phase2RTRequireDelegatePromptOrigin(t, old, original)
			actual, err := s.ContextAttempts(context.Background(), scope, request.RequestID)
			if err != nil || len(actual) != 1 {
				t.Fatal(err, actual)
			}
			supplied := false
			for _, entry := range actual[0].Manifest.Input {
				supplied = supplied || entry.Ref == claim
			}
			if !supplied || !strings.Contains(string(capture.request(t, 0)), "EXCLUDED-CLAIM-871") {
				t.Fatal("claim must actually be supplied before exclusion", actual)
			}
			if err := s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			old = phase2RTWaitRun(t, s, scope, old.ID)
			if old.Status != "failed" || capture.count() != 2 {
				t.Fatal("real first failed deputy HTTP missing", old, capture.count())
			}
			f := &phase2RTRetryFixture{s: s, scope: scope, capture: capture, old: old, origin: phase2RTPromptOriginIDs(t, old)[0]}
			embedding := phase2RTRetryEmbeddingTrap(t, f)
			workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: old.ThingID, MemoryID: string(claim.ID)})
			var exists bool
			if err := s.pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2 AND memory_id=$3)", string(scope.OwnerID), old.ThingID, string(claim.ID)).Scan(&exists); err != nil || !exists {
				t.Fatal("formal destination exclusion failed to persist", err, exists)
			}
			// Exclusion belongs only to this destination; producer/claim permission is
			// still live in a normal independent formal Recall outside this item.
			reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
			recalled := phase2RTRecall(t, s, reader, "排除边界约定代码")
			present := false
			for _, ref := range recalled.Memories {
				present = present || ref == claim
			}
			if !present {
				t.Fatal("claim/producer positive control was revoked instead of item-excluded")
			}
			beforeEmbedding := embedding.count()
			f.negative(t, scope, f.command(t, scope, old.ThingID, "phase2-model", old.Prompt+"。"), memory.ErrConflict)
			if embedding.count() != beforeEmbedding {
				t.Error("excluded generated Prompt reached embedding before final rejection")
			}
			phase2RTEvidence(t, "retry-formal-exclusion", map[string]any{"claim": claim, "secretary_actual_input": actual[0], "original": original, "excluded_thing": old.ThingID, "independent_claim_recall": recalled, "retry_embedding_delta": embedding.count() - beforeEmbedding})
		})
	}
}

func phase2RTRetryContainsOrigin(ids []memory.ID, want memory.ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
func phase2RTRetryDeleteLocator(t *testing.T, f *phase2RTRetryFixture) {
	t.Helper()
	ctx := context.Background()
	var oldRows, tombstonesBefore int
	if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), f.old.ID).Scan(&oldRows); err != nil || oldRows != 1 {
		t.Fatal("deleted locator original Run positive control missing", err, oldRows)
	}
	if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM record_reimport_blocks WHERE owner_id=$1", string(f.scope.OwnerID)).Scan(&tombstonesBefore); err != nil || tombstonesBefore != 0 {
		t.Fatal("unexpected prior deletion tombstones", err, tombstonesBefore)
	}
	oldAttempt := f.old.ContextAttemptID
	if !oldAttempt.Valid() {
		t.Fatal("original failed run has no true diagnostic attempt")
	}
	if err := f.s.Delete(ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{f.source.Ref}, IncludeSources: true, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	var runs, sources, versions, records, tombstones int
	if err := f.s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND id=$2),(SELECT count(*) FROM sources WHERE owner_id=$1 AND id=$3),(SELECT count(*) FROM source_versions WHERE owner_id=$1 AND source_id=$3),(SELECT count(*) FROM memory_records WHERE owner_id=$1 AND id=$3),(SELECT count(*) FROM record_reimport_blocks WHERE owner_id=$1)`, string(f.scope.OwnerID), f.old.ID, string(f.source.ID)).Scan(&runs, &sources, &versions, &records, &tombstones); err != nil || runs != 0 || sources != 0 || versions != 0 || records != 0 || tombstones <= tombstonesBefore {
		t.Fatal("source deletion/gone locator/opaque tombstone not proven", err, runs, sources, versions, records, tombstones)
	}
	gold := phase2RTGoldRead(t)
	reimport, err := memory.NewService(f.s).Ingest(ctx, f.scope, memory.IngestRequest{Connector: "phase2-runtime-synthetic", ExternalID: gold.Records[0].ID, ExternalVersion: "v1", Title: "成都预约资料", Text: gold.Records[0].Text, MediaType: "text/plain"})
	if !errors.Is(err, memory.ErrBlocked) {
		t.Fatal("exact original source reimport was not controlled blocked", err, reimport)
	}
	blockedError := phase2RTRetryError(err)
	body, err := f.s.ContextAttemptSnapshot(ctx, f.scope, oldAttempt)
	if !errors.Is(err, memory.ErrUnavailable) || len(body) != 0 {
		t.Fatal("deleted source original attempt body was available or uncontrolled", err, len(body))
	}
	attempts, err := f.s.ContextAttempts(ctx, f.scope, f.old.ID)
	if err != nil || len(attempts) != 1 || attempts[0].ID != oldAttempt || attempts[0].State != memory.AttemptInvalidated || attempts[0].SnapshotState != memory.SnapshotDeleted {
		t.Fatal("old deleted attempt skeleton is not exactly invalidated/deleted", err, attempts)
	}
	phase2RTEvidence(t, "retry-deleted-locator-and-source-tombstone", map[string]any{"old_run_id": f.old.ID, "old_run_rows": runs, "source_rows": sources, "source_versions": versions, "memory_records": records, "opaque_tombstones_before": tombstonesBefore, "opaque_tombstones_after": tombstones, "reimport_error": blockedError, "original_attempt": attempts[0], "snapshot_bytes": len(body)})
}
