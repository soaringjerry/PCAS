package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func admittedInteractiveRequest(t *testing.T, s *Store, scope memory.Scope, provider string) modelcall.Request {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "UTC"})})
	execution := memory.NewID()
	hash := sha256.Sum256([]byte("Fictitious admitted request."))
	ctx, cancel := context.WithTimeout(context.Background(), heavyUseTimeout)
	defer cancel()
	ticket, err := s.admitSecretaryTurn(ctx, ctx, string(scope.OwnerID), string(execution), string(memory.NewID()), hash[:])
	if err != nil || !ticket.own {
		t.Fatal(ticket, err)
	}
	policy := interactiveCallPolicy{Kind: "secretary", Token: ticket.creator, Origin: fmt.Sprintf("%x", hash), RequestHash: hash[:], AgentID: provider, Usage: modelUsage{ID: memory.NewID(), Tier: "heavy", Purpose: "reader", TurnID: string(memory.NewID()), Plan: asJSON(usePlan{Groups: []string{"self:rule"}})}}
	return modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: execution, RootExecutionID: execution, Function: "reader", Stage: "catalog", ProviderID: provider, Instructions: prompts.Must("memory-reader"), ContextBuilderVersion: "fictitious-reader-v1", Prompt: asJSON(map[string]string{"rawPrompt": "Fictitious catalog input."}), Policy: policy}
}

func TestInteractiveStagesKeepDistinctResultsWithoutAdmissionLockDeadlock(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	lock, err := s.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(context.Background(), `SELECT 1 FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2 FOR UPDATE`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan *modelcall.PaidResult, 2)
	failures := make(chan error, 2)
	for _, stage := range []string{"group:self:rule:0", "group:self:rule:100"} {
		r := request
		r.Stage = stage
		p := r.Policy.(interactiveCallPolicy)
		p.Usage.ID = memory.NewID()
		r.Policy = p
		wg.Add(1)
		go func() {
			defer wg.Done()
			paid, err := s.calls.Call(ctx, r)
			if err != nil {
				failures <- err
			} else {
				results <- paid
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	ids := map[memory.ID]bool{}
	for paid := range results {
		ids[paid.InvocationID] = true
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls c JOIN background_model_results b ON b.owner_id=c.owner_id AND b.job_id=c.id WHERE c.owner_id=$1 AND c.execution_id=$2 AND b.job_id<>c.execution_id AND c.accounting_state='settled'`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&count); err != nil || count != 2 || len(ids) != 2 || calls.Load() != 2 {
		t.Fatal(count, len(ids), calls.Load(), err)
	}
}

func TestInteractiveAccountingRecoveryKeepsOriginalUsageIdentity(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	original := request.Policy.(interactiveCallPolicy).Usage
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_interactive_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic usage failure'; END $$; CREATE TRIGGER reject_interactive_usage BEFORE INSERT ON model_usage FOR EACH ROW EXECUTE FUNCTION reject_interactive_usage()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(ctx, request); err == nil {
		t.Fatal("accounting failure was hidden")
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_interactive_usage ON model_usage`); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	p := request.Policy.(interactiveCallPolicy)
	p.Usage.ID = memory.NewID()
	p.Usage.TurnID = string(memory.NewID())
	p.Usage.Tier = "light"
	request.Policy = p
	paid, err := peer.calls.Call(ctx, request)
	if err != nil || paid == nil || calls.Load() != 1 {
		t.Fatal(paid, err, calls.Load())
	}
	var usageID, turn, tier, state string
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT c.usage_id::text,u.turn_id::text,u.tier,c.accounting_state,(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls c JOIN model_usage u ON u.owner_id=c.owner_id AND u.id=c.usage_id WHERE c.owner_id=$1 AND c.id=$2`, string(scope.OwnerID), string(paid.InvocationID)).Scan(&usageID, &turn, &tier, &state, &count); err != nil || usageID != string(original.ID) || turn != original.TurnID || tier != original.Tier || state != "settled" || count != 1 {
		t.Fatal(usageID, turn, tier, state, count, err)
	}
	if _, err := peer.calls.Call(ctx, request); err != nil || calls.Load() != 1 {
		t.Fatal("duplicate recovery generated again", err, calls.Load())
	}
}

func TestInteractiveResultRejectsChangedInputBindings(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	original := admittedInteractiveRequest(t, s, scope, "model")
	if _, err := s.calls.Call(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*modelcall.Request){
		"payload": func(r *modelcall.Request) {
			r.Prompt = asJSON(map[string]string{"rawPrompt": "Changed fictitious input."})
		},
		"provider":     func(r *modelcall.Request) { r.ProviderID = "another-model" },
		"instructions": func(r *modelcall.Request) { r.Instructions = prompts.Must("secretary") },
		"schema":       func(r *modelcall.Request) { r.Schema = prompts.MustSchema("use-reader") },
		"search":       func(r *modelcall.Request) { r.Search = true },
		"builder":      func(r *modelcall.Request) { r.ContextBuilderVersion = "changed-v2" },
		"root":         func(r *modelcall.Request) { r.RootExecutionID = memory.NewID() },
		"cause":        func(r *modelcall.Request) { r.CausationID = memory.NewID() },
		"refs": func(r *modelcall.Request) {
			r.Refs = []memory.Ref{{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			request := original
			change(&request)
			if _, err := s.calls.Call(context.Background(), request); !errors.Is(err, memory.ErrConflict) {
				t.Fatal(err)
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatal("changed input generated a replacement", calls.Load())
	}
	if _, err := s.pool.Exec(context.Background(), `UPDATE background_model_results SET output='Changed fictitious cached reply.' WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(context.Background(), original); !errors.Is(err, memory.ErrConflict) || calls.Load() != 1 {
		t.Fatal("corrupt cached output was accepted or generated again", err, calls.Load())
	}
}

func TestInteractiveDeletedInputCannotRestorePaidBody(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	ref := organizeTestMemory(t, s, scope, "Fictitious source for a reader.")
	request.Refs = []memory.Ref{ref}
	ctx := context.Background()
	paid, err := s.calls.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	if err := (interactiveCalls{store: s}).Save(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.calls.Call(ctx, request); !errors.Is(err, modelcall.ErrNotApplicable) {
		t.Fatal("deleted input could be applied", err)
	}
	var bodies, usages int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM background_model_results WHERE owner_id=$1),(SELECT count(*) FROM model_usage WHERE owner_id=$1)`, string(scope.OwnerID)).Scan(&bodies, &usages); err != nil || bodies != 0 || usages != 1 || calls.Load() != 1 {
		t.Fatal(bodies, usages, calls.Load(), err)
	}
}

func TestInteractiveUnknownFailureKeepsBudgetAndBoundedRetry(t *testing.T) {
	s, scope := testStore(t), owner()
	dir := secretaryRetryCodex(t, s, 2, 0, "responseStreamDisconnected")
	request := admittedInteractiveRequest(t, s, scope, "chatgpt")
	request.Function = "secretary"
	request.Instructions = prompts.Must("secretary")
	request.Schema = prompts.MustSchema("secretary-output")
	request.Search = true
	request.Stage = "answer"
	p := request.Policy.(interactiveCallPolicy)
	p.Usage.ID = ""
	request.Policy = p
	ctx := context.Background()
	_, err := s.calls.Call(ctx, request)
	var first *modelcall.Failure
	if !errors.Is(err, modelcall.ErrOutcomeUnknown) || !errors.As(err, &first) || !retryableSecretaryModelError(err) {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	_, err = peer.calls.Call(ctx, request)
	var recovered *modelcall.Failure
	if !errors.As(err, &recovered) || recovered.InvocationID != first.InvocationID || !retryableSecretaryModelError(err) {
		t.Fatal("durable failure lost retry metadata", err)
	}
	p.RetryOf = first.InvocationID
	request.Policy = p
	_, err = s.calls.Call(ctx, request)
	var second *modelcall.Failure
	if !errors.Is(err, modelcall.ErrOutcomeUnknown) || !errors.As(err, &second) || second.InvocationID == first.InvocationID {
		t.Fatal(err)
	}
	p.RetryOf = second.InvocationID
	request.Policy = p
	if _, err := s.calls.Call(ctx, request); !errors.Is(err, modelcall.ErrRetryExhausted) {
		t.Fatal("third attempt was not blocked", err)
	}
	var held, usages int
	var reserve float64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE c.accounting_state='held'),(SELECT count(*) FROM model_usage WHERE owner_id=$1),sum(b.reserved_cost::double precision) FROM model_calls c JOIN background_usage b ON b.owner_id=c.owner_id AND b.id=c.reservation_id WHERE c.owner_id=$1`, string(scope.OwnerID)).Scan(&held, &usages, &reserve); err != nil || held != 2 || usages != 2 || reserve <= 0 || len(secretaryRetryCalls(t, dir)) != 2 {
		t.Fatal(held, usages, reserve, err)
	}
	var detail *ai.CodexError
	if !errors.As(second, &detail) || detail.Category != "responseStreamDisconnected" {
		t.Fatal("retry detail disappeared")
	}
}

func TestInteractiveResultWriteFailureReusesPendingResponse(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, "Fictitious reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_interactive_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic result failure'; END $$; CREATE TRIGGER reject_interactive_result BEFORE INSERT ON background_model_results FOR EACH ROW EXECUTE FUNCTION reject_interactive_result()`); err != nil {
		t.Fatal(err)
	}
	limited, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.calls.Call(limited, request); done <- err }()
	pending := false
	for !pending {
		s.pendingInteractive.Range(func(_, value any) bool { pending = true; return false })
		if !pending {
			select {
			case <-limited.Done():
				t.Fatal("provider did not reach result persistence", limited.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	cancel()
	err := <-done
	if err == nil || calls.Load() != 1 {
		t.Fatal("expected one paid call and persistence failure", err, calls.Load())
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_interactive_result ON background_model_results`); err != nil {
		t.Fatal(err)
	}
	s.SetModels(nil)
	paid, err := s.calls.Call(ctx, request)
	if err != nil || paid == nil || calls.Load() != 1 {
		t.Fatal("pending response was not reused", err, calls.Load())
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_usage WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestInteractiveLateResponseFinishesAccountingWithoutApplication(t *testing.T) {
	s, scope := testStore(t), owner()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		secretaryModelReply(w, "Fictitious late reply.")
	})
	request := admittedInteractiveRequest(t, s, scope, "model")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.calls.Call(ctx, request); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := s.pool.Exec(ctx, `UPDATE desk_turn_order SET status='canceled' WHERE owner_id=$1 AND request_id=$2`, string(scope.OwnerID), string(request.ExecutionID)); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, modelcall.ErrNotApplicable) {
		t.Fatal("late response could be applied", err)
	}
	var state string
	var usageCount int
	var available bool
	if err := s.pool.QueryRow(ctx, `SELECT accounting_state,(result_receipt->>'availableForApplication')::boolean,(SELECT count(*) FROM model_usage WHERE owner_id=$1) FROM model_calls WHERE owner_id=$1 AND execution_id=$2`, string(scope.OwnerID), string(request.ExecutionID)).Scan(&state, &available, &usageCount); err != nil || state != "settled" || available || usageCount != 1 || calls.Load() != 1 {
		t.Fatal(state, available, usageCount, calls.Load(), err)
	}
}
