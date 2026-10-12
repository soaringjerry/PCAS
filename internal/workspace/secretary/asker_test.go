package secretary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryModelRetryClassification(t *testing.T) {
	for _, category := range []string{"rateLimitExceeded", "flexUnavailable", "serverOverloaded", "internalServerError", "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts"} {
		for _, status := range []int{0, 408, 429, 500, 502, 503, 504} {
			if !RetryableModelError(fmt.Errorf("wrapped: %w", &ai.CodexError{Category: category, HTTPStatus: status})) {
				t.Errorf("did not retry %s HTTP %d", category, status)
			}
		}
		for _, status := range []int{400, 401, 403, 404, 501, 505} {
			if RetryableModelError(&ai.CodexError{Category: category, HTTPStatus: status}) {
				t.Errorf("retried %s HTTP %d", category, status)
			}
		}
	}
	for _, category := range []string{"contextWindowExceeded", "sessionBudgetExceeded", "usageLimitExceeded", "cyberPolicy", "misalignmentPolicyViolation", "tooManyDenials", "unauthorized", "badRequest", "threadRollbackFailed", "sandboxError", "other", "activeTurnNotSteerable", "unknown", "refresh_token_reused", "refresh_token_expired", "refresh_token_invalidated", "sign_in_required", "transport_error", "event_buffer_exceeded", "turn_failed", "turn_interrupted", "empty_output", "invalid_thread", "invalid_turn"} {
		if RetryableModelError(&ai.CodexError{Category: category}) {
			t.Errorf("retried terminal category %s", category)
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("unclassified"), &ai.CodexError{Category: "serverOverloaded", Cause: context.Canceled}, &modelcall.PersistenceError{Err: &ai.CodexError{Category: "serverOverloaded"}}} {
		if RetryableModelError(err) {
			t.Errorf("retried cancellation/unknown error %v", err)
		}
	}
}

func TestSecretaryModelRetrySharesDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{100 * time.Millisecond, 800 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			deadline, _ := ctx.Deadline()
			calls := 0
			start := time.Now()
			_, err := RetryModel(ctx, func(callCtx context.Context) (ai.Result, error) {
				calls++
				if got, ok := callCtx.Deadline(); !ok || !got.Equal(deadline) {
					t.Fatal("attempt extended the deadline")
				}
				if calls > 1 {
					<-callCtx.Done()
				}
				return ai.Result{}, &ai.CodexError{Category: "serverOverloaded"}
			})
			wantCalls := 1
			if timeout > ModelRetryDelay {
				wantCalls = 2
			}
			if !errors.Is(err, context.DeadlineExceeded) || calls != wantCalls || time.Since(start) > timeout+time.Second {
				t.Fatal("deadline was not respected", calls, err, time.Since(start))
			}
		})
	}
}

// The fake CLI fails real app-server turns, rather than mocking DeskTurn. Its
// state also verifies that both attempts get exactly the same model context.

// fakeAsk is the storage and the stage calls of one asking.
type fakeAsk struct {
	stale  error
	answer string
	fail   error
	calls  []string
	usage  modelcall.Usage
}

func (f *fakeAsk) Agent() workspace.Agent                       { return workspace.Agent{ID: "secretary"} }
func (f *fakeAsk) Verify(context.Context, *Asking) error        { return f.stale }
func (f *fakeAsk) VerifyCurrent(context.Context, *Asking) error { return f.stale }
func (f *fakeAsk) Prepared()                                    {}
func (f *fakeAsk) Reading(time.Duration)                        {}
func (f *fakeAsk) SetTier(context.Context, string) error        { return nil }
func (f *fakeAsk) Call(_, _ context.Context, _ memory.Scope, agent, stage string, _ prompts.Definition, prompt string, _ prompts.Schema, usage modelcall.Usage) (ai.Result, error) {
	f.calls, f.usage = append(f.calls, agent+" "+stage+" "+prompt), usage
	return ai.Result{Text: f.answer}, f.fail
}

func asker(f *fakeAsk) Asker {
	return Asker{Store: f, Stages: f, Request: context.Background(), TurnID: "t1"}
}

var draft = Output{Reply: "草稿", Actions: []Action{{Op: "create_task"}}}

func TestSelfcheckCanDropAnActionOfTheDraftAndIsRecordedWithTheTurn(t *testing.T) {
	f := &fakeAsk{answer: `{"reply":"查过了","used":[],"links":[],"show":[],"actions":[{"op":"skip"}],"ask":null}`}
	state := &Asking{Tier: "medium", Input: "虚构输入", Dependencies: []memory.Ref{{ID: "m1", Version: 1}}}
	state.Use.Groups = []string{"self:goal"}
	out, err := asker(f).Check(context.Background(), time.Second, state, draft)
	if err != nil || len(out.Actions) != 1 || !out.Actions[0].SelfcheckDropped {
		t.Fatal(err, out)
	}
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "secretary selfcheck 虚构输入\n待自查的草稿：\n{") {
		t.Fatal(f.calls)
	}
	if f.usage.Purpose != "selfcheck" || f.usage.Tier != "medium" || f.usage.TurnID != "t1" || len(f.usage.MemoryRefs) != 1 || string(f.usage.Plan) != `{"groups":["self:goal"]}` {
		t.Fatal(f.usage, string(f.usage.Plan))
	}
}

func TestSelfcheckThatDoesNotFinishKeepsTheDraft(t *testing.T) {
	for name, f := range map[string]*fakeAsk{
		"changed context": {stale: memory.ErrConflict},
		"failed call":     {fail: errors.New("fictitious failure")},
		"unreadable":      {answer: "Fictitious prose."},
	} {
		out, err := asker(f).Check(context.Background(), time.Second, &Asking{}, draft)
		if err == nil || out.Reply != "草稿" || len(out.Actions) != 1 || out.Actions[0].SelfcheckDropped {
			t.Fatal(name, err, out)
		}
		if name == "changed context" && len(f.calls) != 0 {
			t.Fatal("a check of a changed context was paid", f.calls)
		}
	}
	// A check with no time left makes no call.
	f := &fakeAsk{}
	if _, err := asker(f).Check(context.Background(), 0, &Asking{}, draft); !errors.Is(err, context.DeadlineExceeded) || len(f.calls) != 0 {
		t.Fatal(err, f.calls)
	}
}

func TestAnswerCallNeedsTheAdmittedExecutionOfASecretaryTurn(t *testing.T) {
	_, stage, failure, err := asker(&fakeAsk{}).Generate(context.Background(), &Asking{})
	if err != nil || !errors.Is(failure, memory.ErrInvalid) || stage != "verify" {
		t.Fatal(stage, failure, err)
	}
	a := asker(&fakeAsk{})
	a.Request = modelcall.WithExecution(context.Background(), modelcall.Request{Policy: modelcall.InteractivePolicy{Kind: "deputy"}})
	if _, stage, failure, _ := a.Generate(context.Background(), &Asking{}); !errors.Is(failure, memory.ErrInvalid) || stage != "verify" {
		t.Fatal("a deputy's execution made a secretary answer call", stage, failure)
	}
}
