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
		c.mu.Lock()
		c.Reply = string(asJSON(map[string]any{"reply": "RETRY-SECRETARY-863", "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": []any{map[string]any{"op": "delegate", "ref": "new", "title": "Retry source task " + gold.Records[0].Atoms[0], "prompt": "RETRY-GENERATED-PROMPT-864 整理成都预约资料，预约码 " + gold.Records[0].Atoms[0], "kind": "draft"}}}))
		c.mu.Unlock()
		request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: gold.Cases[0].Query + "请让副手整理资料"}
		if studio {
			request.ThingID = &f.studio
		}
		out, err := s.DeskTurn(context.Background(), scope, request)
		if err != nil || c.count() != 1 {
			t.Fatal("retry real secretary/delegation positive control missing", err, c.count(), out)
		}
		f.old = phase2RTDelegatedRun(t, out)
		phase2RTRequireDelegatePromptOrigin(t, f.old, out)
		f.origin = phase2RTPromptOriginIDs(t, f.old)[0]
		phase2RTSnapshotForSource(t, s, scope, f.source.Ref, "secretary", request.RequestID, c.request(t, 0))
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
		if len(attempt.Manifest.DeskActions) != 1 || attempt.Manifest.DeskActions[0] != f.origin {
			t.Error("new attempt lacks original delegate action")
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
				phase2RTAuthorize(t, f.s, f.scope, f.source.Ref, agent, "deputy", hard)
				workspaceCommand(t, f.s, f.scope, workspace.Command{Type: "moveThing", ID: f.old.ThingID, ProjectID: b})
				f.negative(t, f.scope, f.command(t, f.scope, f.old.ThingID, agent, prompt), memory.ErrForbidden)
			case "RETRY-OWNER-INDEPENDENT":
				phase2RTUpdatePolicy(t, f.s, f.scope, f.source.Ref, f.secretary, true)
				var original workspace.Run
				if err := f.s.pool.QueryRow(context.Background(), "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), f.old.ID).Scan(&original); err != nil || !original.StaleContext || original.Prompt != prompt {
					t.Fatal("owner old general-context stale positive control missing", err, original)
				}
				f.success(t, f.command(t, f.scope, f.old.ThingID, agent, prompt), f.deputy, true)
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
					if err := f.s.Delete(context.Background(), f.scope, memory.DeleteRequest{Targets: []memory.Ref{f.source.Ref}, IncludeSources: true, BlockReimport: true}); err != nil {
						t.Fatal(err)
					}
				default:
					t.Fatal("unknown frozen retry case", tc.ID)
				}
				f.negative(t, f.scope, f.command(t, f.scope, f.old.ThingID, agent, prompt), memory.ErrConflict)
			}
		})
	}
}

func TestPhase2RuntimeRetryManualAndFinalFence(t *testing.T) {
	for _, name := range []string{"RETRY-MANUAL-LIVE", "RETRY-MANUAL-REVOKED", "RETRY-FINAL-BARRIER"} {
		t.Run(name, func(t *testing.T) {
			f := phase2RTRetrySetup(t, false, false)
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

func phase2RTRetryFinalBarrier(t *testing.T, f *phase2RTRetryFixture) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	var raw []byte
	var enteredAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		enteredAt = time.Now().UTC()
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(finish)
	f.capture.Registry.Config.Embedding = "phase2-retry-embedding"
	f.capture.Registry.Config.Providers = append(f.capture.Registry.Config.Providers, ai.Provider{ID: "phase2-retry-embedding", Protocol: "openai", BaseURL: server.URL, Model: "synthetic-vector", Embedding: true, CostMode: "free"})
	command := f.command(t, f.scope, f.old.ThingID, "phase2-model", f.old.Prompt+"。")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	type result struct {
		state workspace.State
		err   error
	}
	done := make(chan result, 1)
	go func() { st, err := f.s.Execute(ctx, f.scope, command); done <- result{st, err} }()
	select {
	case <-entered:
	case r := <-done:
		t.Fatal("public retry never entered external embedding barrier", r.err)
	case <-ctx.Done():
		t.Fatal("public embedding barrier not reached")
	}
	if !strings.Contains(string(raw), "RETRY-GENERATED-PROMPT-864") || !strings.Contains(string(raw), phase2RTGoldRead(t).Records[0].Atoms[0]) {
		t.Error("external prepared query positive control lost original generated prompt", string(raw))
	}
	// Revoke through the real policy API while the external query is suspended.
	revoked := phase2RTUpdatePolicy(t, f.s, f.scope, f.source.Ref, f.secretary, true)
	revokedAt := time.Now().UTC()
	if !revoked.Authorization.Revoked || !revokedAt.After(enteredAt) {
		t.Fatal("formal source revoke did not commit during external preparation", revoked)
	}
	select {
	case r := <-done:
		t.Fatal("Execute returned before public embedding barrier was released", r.err)
	default:
	}
	finish()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		t.Fatal("retry final stage did not return after embedding release")
	}
	if !errors.Is(r.err, memory.ErrConflict) || f.capture.count() != 2 {
		t.Fatal("final retry fence failed to reject source revocation", r.err, f.capture.count())
	}
	var count int
	if err := f.s.pool.QueryRow(context.Background(), "SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), command.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("final revoked retry committed a new Run", err, count)
	}
	phase2RTEvidence(t, "retry-final-public-embedding-barrier", map[string]any{"command": command, "actual_embedding_request": json.RawMessage(raw), "embedding_entered_at": enteredAt, "formal_revoke_returned_at": revokedAt, "revoked_policy_receipt": revoked, "error": phase2RTRetryError(r.err), "new_runs": count, "deputy_http_delta": f.capture.count() - 2})
}
