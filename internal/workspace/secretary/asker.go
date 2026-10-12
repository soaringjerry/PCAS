package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	// ModelTimeout is the time that the answer call of a turn has, with its
	// retry.
	ModelTimeout = 30 * time.Second
	// ModelAttempts is the number of answer calls of one turn at most.
	ModelAttempts = 2
	// ModelRetryDelay is the wait before the second answer call.
	ModelRetryDelay = 500 * time.Millisecond
	// TurnTimeout is the time that a turn has with heavy reading.
	TurnTimeout = 3 * time.Minute
)

// AskStore is what the asking of one turn reads and records in storage.
type AskStore interface {
	// Agent is the agent that answers the turn.
	Agent() workspace.Agent
	// Verify checks in the transaction of the turn that the agent may still
	// use what the turn holds.
	Verify(ctx context.Context, state *Asking) error
	// VerifyCurrent makes the same check against committed data.
	VerifyCurrent(ctx context.Context, state *Asking) error
	// Prepared marks the end of the preparation, and Reading adds reading
	// time to it. They feed the timing record only.
	Prepared()
	Reading(d time.Duration)
	// SetTier records the tier that the turn reached.
	SetTier(ctx context.Context, tier string) error
}

// StageCalls makes one stage call of the execution that ctx carries.
type StageCalls interface {
	Call(ctx, persist context.Context, scope memory.Scope, agent, stage string, instructions prompts.Definition, prompt string, schema prompts.Schema, usage modelcall.Usage) (ai.Result, error)
}

// Reader reads groups of memory for a task.
type Reader interface {
	Read(ctx, persist context.Context, task reader.Task, u contextbuilder.UseContext) ([]workspace.Memory, []memory.Ref, []string)
}

// Asker makes the reads and the model calls of one turn. It is the Model of
// the turn. A call ends with the caller's request; its record is written
// under the turn's own context.
type Asker struct {
	Store  AskStore
	Calls  *modelcall.Gateway
	Stages StageCalls
	Reader Reader
	// Request is the context of the caller's request.
	Request context.Context
	Scope   memory.Scope
	// Thing is the open item, or nil.
	Thing  *string
	TurnID string
}

func (m Asker) HeavyRead(ctx context.Context, deadline time.Time, task string, state *Asking, stage string) ([]workspace.Memory, []memory.Ref, []string) {
	readCtx := m.Request
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		readCtx, cancel = context.WithDeadline(m.Request, deadline)
		defer cancel()
	}
	return m.Reader.Read(readCtx, ctx, reader.Task{Scope: m.Scope, Agent: m.Store.Agent(), Thing: m.Thing, Text: task, Turn: m.TurnID, Phase: stage}, state.Use)
}

func (m Asker) Prepared()               { m.Store.Prepared() }
func (m Asker) Reading(d time.Duration) { m.Store.Reading(d) }

func (m Asker) SetTier(ctx context.Context, tier string) error { return m.Store.SetTier(ctx, tier) }

func usePlan(groups []string) json.RawMessage {
	raw, _ := json.Marshal(UsePlan{Groups: groups})
	return raw
}

// Generate makes the answer call. All calls and the wait share one deadline;
// an attempt never gets a fresh timeout. Before a second call the turn's
// context is verified again.
func (m Asker) Generate(_ context.Context, state *Asking) (string, string, error, error) {
	workCtx, cancel := context.WithTimeout(m.Request, ModelTimeout)
	defer cancel()
	agent := m.Store.Agent().ID
	request, ok := modelcall.Execution(workCtx)
	if !ok || request.OwnerID != m.Scope.OwnerID {
		return "", "verify", memory.ErrInvalid, nil
	}
	policy, ok := request.Policy.(modelcall.InteractivePolicy)
	if !ok || policy.Kind != "secretary" {
		return "", "verify", memory.ErrInvalid, nil
	}
	policy.AgentID = agent
	policy.Usage = modelcall.Usage{Tier: state.Tier, TurnID: m.TurnID, MemoryRefs: state.Dependencies, Plan: usePlan(state.Use.Groups), Purpose: "secretary"}
	// A memory correction can make the reply stale, but cannot discard it.
	// Current grants, exclusions, source existence, and destination rules still apply.
	policy.InputPolicy = "current_access"
	request.Policy, request.Stage, request.ProviderID = policy, "answer", agent
	request.Instructions, request.Schema = prompts.Must("secretary"), prompts.MustSchema("secretary-output")
	// The reply parser validates the output, so an ordinary provider can answer.
	request.Search, request.ModeFallback, request.ContextBuilderVersion = true, true, "secretary-answer-v1"
	record, _ := json.Marshal(map[string]string{"rawPrompt": state.Input})
	request.Prompt, request.ModelInput = record, state.Input
	request.Refs = uniqueRefs(state.Dependencies)
	stage, attempt := "model", 0
	var accountingErr error
	result, err := RetryModel(workCtx, func(callCtx context.Context) (ai.Result, error) {
		attempt++
		if attempt > 1 {
			stage = "verify"
			if err := m.Store.Verify(callCtx, state); err != nil {
				return ai.Result{}, err
			}
		}
		stage = "model"
		type returned struct {
			paid *modelcall.PaidResult
			err  error
		}
		finished := make(chan returned, 1)
		bound := request
		go func() {
			paid, err := m.Calls.Call(modelcall.Timed(callCtx, "answer"), bound)
			finished <- returned{paid, err}
		}()
		select {
		case <-callCtx.Done():
			return ai.Result{}, callCtx.Err()
		case outcome := <-finished:
			var persistenceErr *modelcall.PersistenceError
			if errors.As(outcome.err, &persistenceErr) {
				accountingErr = outcome.err
				return ai.Result{}, outcome.err
			}
			if callCtx.Err() != nil {
				return ai.Result{}, callCtx.Err()
			}
			if outcome.err != nil {
				if errors.Is(outcome.err, workspace.ErrBudget) {
					stage = "budget"
				}
				if errors.Is(outcome.err, memory.ErrConflict) || errors.Is(outcome.err, memory.ErrForbidden) || errors.Is(outcome.err, modelcall.ErrNotApplicable) {
					stage = "verify"
				}
				if errors.Is(outcome.err, modelcall.ErrNotApplicable) {
					// Input access was lost after submission. Keep the existing
					// context-change receipt and retain the gateway rejection cause.
					return ai.Result{}, errors.Join(memory.ErrForbidden, outcome.err)
				}
				var failure *modelcall.Failure
				if errors.As(outcome.err, &failure) && RetryableModelError(outcome.err) && failure.InvocationID.Valid() {
					policy.RetryOf = failure.InvocationID
					request.Policy = policy
				}
				return ai.Result{}, outcome.err
			}
			paid := outcome.paid
			return ai.Result{Text: paid.Output, Searches: paid.Searches, DurationMS: paid.DurationMS, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost}, nil
		}
	})
	// An accounting failure ends the request, as it did before retries.
	if accountingErr != nil {
		return "", stage, nil, accountingErr
	}
	return result.Text, stage, err, nil
}

// Check lets the model check its answer within the given time. A check that
// does not finish keeps the answer.
func (m Asker) Check(ctx context.Context, timeout time.Duration, state *Asking, before Output) (Output, error) {
	checkCtx, cancel := context.WithTimeout(m.Request, timeout)
	defer cancel()
	if checkCtx.Err() != nil {
		return before, checkCtx.Err()
	}
	if err := m.Store.VerifyCurrent(checkCtx, state); err != nil {
		return before, err
	}
	draft, _ := json.Marshal(before)
	raw, err := m.Stages.Call(checkCtx, ctx, m.Scope, m.Store.Agent().ID, "selfcheck", prompts.Must("secretary-selfcheck"), state.Input+"\n待自查的草稿：\n"+string(draft), prompts.MustSchema("secretary-check"), modelcall.Usage{Purpose: "selfcheck", Tier: state.Tier, TurnID: m.TurnID, MemoryRefs: state.Dependencies, Plan: usePlan(state.Use.Groups)})
	if err == nil {
		var after Output
		after, err = ParseOutput(raw.Text)
		if err == nil {
			return ConstrainCheck(before, after), nil
		}
	}
	slog.WarnContext(ctx, "memory selfcheck fallback", "stage", "selfcheck", "error_type", ErrorType("selfcheck", err))
	return before, err
}

// RetryableModelError reports whether an answer call can be made again. Only
// identified transient Codex failures are retried. Authentication, refusal,
// exhausted quotas, unknown failures and unclassified providers stay terminal.
func RetryableModelError(err error) bool {
	var persistenceErr *modelcall.PersistenceError
	if errors.As(err, &persistenceErr) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var detail *ai.CodexError
	if !errors.As(err, &detail) {
		return false
	}
	if detail.HTTPStatus != 0 {
		switch detail.HTTPStatus {
		case 408, 429, 500, 502, 503, 504:
		default:
			return false
		}
	}
	switch detail.Category {
	case "rateLimitExceeded", "flexUnavailable", "serverOverloaded", "internalServerError",
		"httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected",
		"responseTooManyFailedAttempts":
		return true
	default:
		return false
	}
}

// RetryModel makes the answer call and, after a transient failure, makes it
// once more. call owns each invocation's budget accounting. Actions, original
// capture and the saved response remain outside this loop. All calls and the
// wait share the caller's deadline; an attempt never gets a fresh timeout.
func RetryModel(ctx context.Context, call func(context.Context) (ai.Result, error)) (ai.Result, error) {
	started := time.Now()
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return ai.Result{}, err
		}
		result, err := call(ctx)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err == nil {
			return result, nil
		}
		if attempt >= ModelAttempts || !RetryableModelError(err) {
			return result, err
		}
		var detail *ai.CodexError
		_ = errors.As(err, &detail)
		slog.WarnContext(ctx, "secretary model retry", "attempt", attempt,
			"max_attempts", ModelAttempts, "category", detail.Category,
			"http_status", detail.HTTPStatus, "retry_delay_ms", ModelRetryDelay.Milliseconds(),
			"elapsed_ms", time.Since(started).Milliseconds())
		timer := time.NewTimer(ModelRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}
