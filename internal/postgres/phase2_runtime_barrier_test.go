package postgres

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type phase2RTLifecycleGold struct {
	DerivedReply string `json:"derived_reply"`
	Replacement  struct {
		ExternalVersion string   `json:"external_version"`
		Text            string   `json:"text"`
		Atoms           []string `json:"atoms"`
	} `json:"replacement"`
}

func phase2RTDerivedReply(t *testing.T, capture *phase2RTCapture) string {
	t.Helper()
	var gold phase2RTLifecycleGold
	phase2RTReadJSON(t, "runtime-sequences.json", &gold)
	capture.mu.Lock()
	capture.Reply = string(asJSON(map[string]any{"reply": gold.DerivedReply, "used": []string{}, "links": []any{}, "show": []any{}, "remember": false, "actions": []any{}, "ask": nil}))
	capture.mu.Unlock()
	return gold.DerivedReply
}

func phase2RTMutation(ctx context.Context, s *Store, scope memory.Scope, source memory.Ref, grant phase2RTAuthResult, mutation string, gold phase2RTLifecycleGold) error {
	switch mutation {
	case "revoke":
		writer, ok := any(s).(memory.SourceAuthorizer)
		if !ok {
			return errors.New("actual SourceAuthorizer missing")
		}
		_, err := writer.SetSourceAuthorization(ctx, scope, memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source, PolicyID: grant.Authorization.ID, ExpectedPolicyRevision: grant.Authorization.Revision, Recipient: grant.Authorization.Recipient, Purpose: grant.Authorization.Purpose, Scope: grant.Authorization.Scope, Revoke: true})
		return err
	case "delete":
		return s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{source}, IncludeSources: true, BlockReimport: true})
	case "replace":
		_, err := memory.NewService(s).Ingest(ctx, scope, memory.IngestRequest{Connector: "phase2-runtime-synthetic", ExternalID: "phase2-raw", ExternalVersion: gold.Replacement.ExternalVersion, Title: "成都预约资料", Text: gold.Replacement.Text, MediaType: "text/plain"})
		return err
	default:
		return memory.ErrInvalid
	}
}

func phase2RTWaitInvalidRun(t *testing.T, s *Store, scope memory.Scope, id string) *workspace.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := s.Snapshot(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		var found *workspace.Run
		for i := range state.Runs {
			if state.Runs[i].ID == id {
				found = &state.Runs[i]
				break
			}
		}
		if found == nil {
			return nil
		}
		if found.Status == "done" || found.Status == "failed" {
			return found
		}
		select {
		case <-ctx.Done():
			t.Fatal("old provider result did not terminate after mutation/release")
		case <-ticker.C:
		}
	}
}

func TestPhase2RuntimeProviderBarrierMutationCommitsAndOldResultRejected(t *testing.T) {
	for _, entry := range []string{"DeskTurn", "RunAgents"} {
		for _, mutation := range []string{"replace", "delete", "revoke"} {
			t.Run(entry+"_"+mutation, func(t *testing.T) {
				s, scope, capture := phase2RTSetup(t)
				source := phase2RTSource(t, s, scope)
				role := "secretary"
				if entry == "RunAgents" {
					role = "deputy"
				}
				grant, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", role, phase2RTUnscoped())
				oldDerived := phase2RTDerivedReply(t, capture)
				var gold phase2RTLifecycleGold
				phase2RTReadJSON(t, "runtime-sequences.json", &gold)
				entered, release := capture.barrier(t)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				type deskResult struct {
					Out workspace.DeskTurnResponse
					Err error
				}
				deskDone := make(chan deskResult, 1)
				var run workspace.Run
				if entry == "DeskTurn" {
					go func() {
						out, err := s.DeskTurn(ctx, scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
						deskDone <- deskResult{out, err}
					}()
				} else {
					run = phase2RTPrepareRun(t, s, scope, "phase2-model", phase2RTGoldRead(t).Cases[0].Query)
					phase2RTStartRunner(t, s)
				}
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("actual provider HTTP receive barrier not reached")
				}
				phase2RTAtoms(t, capture.request(t, 0), phase2RTGoldRead(t).Cases[0].Required, true)
				mutationCtx, mutationCancel := context.WithTimeout(context.Background(), 2*time.Second)
				start := time.Now()
				mutationErr := phase2RTMutation(mutationCtx, s, scope, source.Ref, grant, mutation, gold)
				elapsed := time.Since(start)
				mutationCancel()
				phase2RTEvidence(t, "mutation-held-provider", map[string]any{"mutation": mutation, "error": mutationErr != nil, "elapsed_ms": elapsed.Milliseconds(), "provider_received_before_mutation": true, "provider_response_still_held": true})
				if mutationErr != nil {
					t.Errorf("source mutation did not commit while actual provider remained held: %v", mutationErr)
				}
				release()
				if entry == "DeskTurn" {
					select {
					case result := <-deskDone:
						phase2RTEvidence(t, "late-desk-result", map[string]any{"out": result.Out, "error": result.Err != nil})
						if strings.Contains(result.Out.Turn.Reply, oldDerived) {
							t.Error("invalid old secretary result was returned as current answer")
						}
					case <-time.After(8 * time.Second):
						t.Fatal("DeskTurn did not finish after provider release")
					}
				} else {
					finished := phase2RTWaitInvalidRun(t, s, scope, run.ID)
					phase2RTEvidence(t, "late-run-result", finished)
					if finished != nil && finished.Status == "done" && !finished.StaleContext {
						t.Error("invalid source-backed old run remained successful/applicable")
					}
					state, err := s.Snapshot(context.Background(), scope)
					if err != nil {
						t.Fatal(err)
					}
					_, err = s.Execute(context.Background(), scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "doc", Text: "attempt to adopt invalid old synthetic output", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
					if err == nil {
						t.Error("source changed during provider generation but old result adoption succeeded")
					}
				}
				diagnostics, ok := any(s).(phase2RTDiagnostics)
				if !ok {
					t.Fatal("actual ContextAttempts missing")
				}
				attempts, err := diagnostics.ContextAttempts(context.Background(), scope, "")
				if err != nil {
					t.Fatal(err)
				}
				invalidated := false
				for _, attempt := range attempts {
					if attempt.Manifest.Recipient.Role == role && attempt.DispatchedAt != nil && attempt.State == memory.AttemptInvalidated {
						invalidated = true
						if mutation != "replace" {
							body, _ := diagnostics.ContextAttemptSnapshot(context.Background(), scope, attempt.ID)
							if len(body) != 0 {
								t.Error("deleted/revoked source retained diagnostic sent body")
							}
						}
					}
				}
				if !invalidated {
					t.Error("actual sent attempt disappeared or did not retain invalidated dispatch evidence")
				}
			})
		}
	}
}

func TestPhase2RuntimeRevokedHistoryNotReplayedSameOrNewConversation(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	grant, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	oldDerived := phase2RTDerivedReply(t, capture)
	first, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
	if err != nil {
		t.Fatal(err)
	}
	if first.Turn.Reply != oldDerived {
		t.Fatal("lawful prior derived reply fixture was not created")
	}
	phase2RTAtoms(t, capture.request(t, 0), phase2RTGoldRead(t).Cases[0].Required, true)
	phase2RTUpdatePolicy(t, s, scope, source.Ref, grant, true)
	capture.mu.Lock()
	capture.Reply = `{"reply":"下一轮合成回执","used":[],"links":[],"show":[],"remember":false,"actions":[],"ask":null}`
	capture.mu.Unlock()
	for _, same := range []bool{true, false} {
		request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: "继续之前的预约资料工作"}
		if same {
			request.ConversationID = &first.ConversationID
		}
		before := capture.count()
		out, err := s.DeskTurn(context.Background(), scope, request)
		if err != nil {
			t.Fatal(err)
		}
		phase2RTEvidence(t, "followup-"+string(memory.NewID()), map[string]any{"same_conversation": same, "response": out})
		payload := capture.request(t, before)
		phase2RTAtoms(t, payload, phase2RTGoldRead(t).Cases[0].Required, false)
		if strings.Contains(string(payload), oldDerived) {
			t.Error("invalid prior derived secretary output replayed into new model input")
		}
	}
	w := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/desk/turns?conversationId="+first.ConversationID, nil)
	if strings.Contains(w.Body.String(), oldDerived) {
		t.Error("revoked source-derived old history still exposed as live replayable answer")
	}
	var metadata string
	if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg(manifest)::text,'[]') FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	phase2RTAtoms(t, []byte(metadata), phase2RTGoldRead(t).Cases[0].Required, false)
}

func TestPhase2RuntimeActualAttemptSurvivesProviderCancellation(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	entered, release := capture.barrier(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requestID := string(memory.NewID())
	done := make(chan error, 1)
	go func() {
		_, err := s.DeskTurn(ctx, scope, workspace.DeskTurnRequest{RequestID: requestID, AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual provider receive not reached")
	}
	phase2RTAtoms(t, capture.request(t, 0), phase2RTGoldRead(t).Cases[0].Required, true)
	cancel()
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled actual Desk request failed to terminate")
	}
	diagnostics, ok := any(s).(phase2RTDiagnostics)
	if !ok {
		t.Fatal("actual diagnostic API missing")
	}
	attempts, err := diagnostics.ContextAttempts(context.Background(), scope, requestID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("real attempted send lost on business cancellation: diagnostic attempts=%d want1", len(attempts))
	}
	attempt := attempts[0]
	phase2RTEvidence(t, "canceled-attempt", attempt)
	if attempt.DispatchedAt == nil || attempt.State == memory.AttemptPrepared || attempt.State == memory.AttemptCompleted {
		t.Error("sent canceled request was misreported as merely prepared or completed")
	}
	if capture.count() != 1 {
		t.Error("business cancellation silently repeated the provider request")
	}
	var responses int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM desk_turns WHERE owner_id=$1 AND request_id=$2 AND answer<>''", string(scope.OwnerID), requestID).Scan(&responses); err != nil {
		t.Fatal(err)
	}
	if responses != 0 {
		t.Error("canceled business request saved a fake successful answer")
	}
}
