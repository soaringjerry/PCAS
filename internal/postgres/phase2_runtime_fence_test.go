package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2RuntimeBeforeDispatchFencePreventsKnownInvalidationSend(t *testing.T) {
	for _, entry := range []string{"DeskTurn", "RunAgents"} {
		for _, mutation := range []string{"replace", "delete", "revoke", "route"} {
			t.Run(entry+"_"+mutation, func(t *testing.T) {
				s, scope, capture := phase2RTSetup(t)
				source := phase2RTSource(t, s, scope)
				role := "secretary"
				if entry == "RunAgents" {
					role = "deputy"
				}
				grant, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", role, phase2RTUnscoped())
				var gold phase2RTLifecycleGold
				phase2RTReadJSON(t, "runtime-sequences.json", &gold)
				entered := make(chan memory.ContextRequestEvent, 1)
				release := make(chan struct{})
				var once sync.Once
				finish := func() { once.Do(func() { close(release) }) }
				t.Cleanup(finish)
				capture.Registry.ContextObserver = memory.ContextRequestObserverFunc(func(ctx context.Context, event memory.ContextRequestEvent) error {
					if event.Stage != "before_dispatch" {
						return nil
					}
					event.Payload = append([]byte(nil), event.Payload...)
					select {
					case entered <- event:
					case <-ctx.Done():
						return ctx.Err()
					}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				done := make(chan error, 1)
				var run workspace.Run
				operationID := string(memory.NewID())
				if entry == "DeskTurn" {
					go func() {
						_, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: operationID, AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
						done <- err
					}()
				} else {
					run = phase2RTPrepareRun(t, s, scope, "phase2-model", phase2RTGoldRead(t).Cases[0].Query)
					operationID = run.ID
					phase2RTStartRunner(t, s)
				}
				var event memory.ContextRequestEvent
				select {
				case event = <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("real final-payload before_dispatch boundary not reached")
				}
				phase2RTAtoms(t, event.Payload, phase2RTGoldRead(t).Cases[0].Required, true)
				if event.ObservationLayer != "serialized_request" || capture.count() != 0 {
					t.Fatal("before_dispatch observation confused with actual provider receipt")
				}
				diagnostics, ok := any(s).(phase2RTDiagnostics)
				if !ok {
					t.Fatal("actual diagnostic API missing")
				}
				attempts, err := diagnostics.ContextAttempts(context.Background(), scope, operationID)
				if err != nil || len(attempts) != 1 {
					t.Fatalf("prepared independent diagnostic not retained before send: rows=%d error=%v", len(attempts), err)
				}
				attempt := attempts[0]
				if attempt.DispatchedAt != nil {
					t.Error("reserved but unattempted send has fabricated dispatched_at")
				}
				mutationCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				start := time.Now()
				if mutation == "route" {
					capture.Registry.Config.Providers[0].Model = "phase2-route-rebound"
				} else {
					err = phase2RTMutation(mutationCtx, s, scope, source.Ref, grant, mutation, gold)
				}
				cancel()
				mutationElapsed := time.Since(start).Milliseconds()
				if err != nil {
					t.Fatalf("mutation before dispatch must commit without provider release: %v", err)
				}
				finish()
				if entry == "DeskTurn" {
					select {
					case err = <-done:
						if errors.Is(err, context.DeadlineExceeded) {
							t.Error("known invalidation was handled only by unrelated timeout")
						}
					case <-time.After(5 * time.Second):
						t.Fatal("invalidated Desk attempt did not terminate")
					}
				} else {
					phase2RTWaitInvalidRun(t, s, scope, run.ID)
				}
				if capture.count() != 0 {
					t.Error("known source/recipient invalidation committed before fence but request reached provider")
				}
				attempts, err = diagnostics.ContextAttempts(context.Background(), scope, attempt.OperationID)
				if err != nil || len(attempts) != 1 {
					t.Fatalf("unsent prepared history lost: rows=%d error=%v", len(attempts), err)
				}
				final := attempts[0]
				if final.DispatchedAt != nil || final.State == memory.AttemptCompleted || final.State == memory.AttemptDispatched || final.State == memory.AttemptPrepared {
					t.Error("canceled final-fence attempt has false send/completion or unresolved prepared state")
				}
				phase2RTEvidence(t, "before-dispatch-fence", map[string]any{"entry": entry, "mutation": mutation, "prepared_attempt": attempt, "final_attempt": final, "event_provider": event.ProviderID, "event_model": event.Model, "payload_bytes": len(event.Payload), "mutation_elapsed_ms": mutationElapsed, "actual_provider_requests": capture.count()})
			})
		}
	}
}
