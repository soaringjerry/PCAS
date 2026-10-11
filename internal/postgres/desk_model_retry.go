package postgres

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Accounting failures abort the result transaction, as they did before retries.
type secretaryAccountingError struct{ error }
type secretaryUsageKey struct{}
type secretaryUsageMeta struct {
	Usage modelUsage
}

func (s *Store) generateSecretaryModelWithRetry(workCtx context.Context, scope memory.Scope, agentID, prompt string, verify func(context.Context) error) (ai.Result, string, error) {
	request, ok := workCtx.Value(interactiveExecutionKey{}).(modelcall.Request)
	meta, hasUsage := workCtx.Value(secretaryUsageKey{}).(secretaryUsageMeta)
	if !ok || !hasUsage || request.OwnerID != scope.OwnerID {
		return ai.Result{}, "verify", memory.ErrInvalid
	}
	policy, ok := request.Policy.(interactiveCallPolicy)
	if !ok || policy.Kind != "secretary" {
		return ai.Result{}, "verify", memory.ErrInvalid
	}
	policy.AgentID, policy.Usage = agentID, meta.Usage
	policy.Usage.Purpose = "secretary"
	// A memory correction can make the reply stale, but cannot discard it.
	// Current grants, exclusions, source existence, and destination rules still apply.
	policy.InputPolicy = "current_access"
	request.Policy, request.Stage, request.ProviderID = policy, "answer", agentID
	request.Instructions, request.Schema = prompts.Must("secretary"), prompts.MustSchema("secretary-output")
	// The reply parser validates the output, so an ordinary provider can answer.
	request.Search, request.ModeFallback, request.ContextBuilderVersion = true, true, "secretary-answer-v1"
	request.Prompt = asJSON(map[string]string{"rawPrompt": prompt})
	request.Refs = uniqueRefs(meta.Usage.MemoryRefs)
	stage, attempt := "model", 0
	var accountingErr error
	result, err := retrySecretaryModel(workCtx, func(callCtx context.Context) (ai.Result, error) {
		attempt++
		if attempt > 1 {
			stage = "verify"
			if err := verify(callCtx); err != nil {
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
			paid, err := s.calls.Call(executionCallContext(callCtx, "answer"), bound)
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
				if errors.As(outcome.err, &failure) && retryableSecretaryModelError(outcome.err) && failure.InvocationID.Valid() {
					policy.RetryOf = failure.InvocationID
					request.Policy = policy
				}
				return ai.Result{}, outcome.err
			}
			paid := outcome.paid
			return ai.Result{Text: paid.Output, Searches: paid.Searches, DurationMS: paid.DurationMS, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost}, nil
		}
	})
	if accountingErr != nil {
		return result, stage, &secretaryAccountingError{accountingErr}
	}
	return result, stage, err
}

const (
	secretaryModelTimeout    = 30 * time.Second
	secretaryModelAttempts   = 2
	secretaryModelRetryDelay = 500 * time.Millisecond
)

// Retry only identified transient Codex failures. Authentication, refusal,
// exhausted quotas, unknown failures and unclassified providers stay terminal.
func retryableSecretaryModelError(err error) bool {
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

// call owns each invocation's budget accounting. Actions, original capture and
// the saved response remain outside this loop. All calls and the wait share the
// caller's deadline; an attempt never gets a fresh timeout.
func retrySecretaryModel(ctx context.Context, call func(context.Context) (ai.Result, error)) (ai.Result, error) {
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
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if attempt >= secretaryModelAttempts || !retryableSecretaryModelError(err) {
			return result, err
		}
		var detail *ai.CodexError
		_ = errors.As(err, &detail)
		slog.WarnContext(ctx, "secretary model retry", "attempt", attempt,
			"max_attempts", secretaryModelAttempts, "category", detail.Category,
			"http_status", detail.HTTPStatus, "retry_delay_ms", secretaryModelRetryDelay.Milliseconds(),
			"elapsed_ms", time.Since(started).Milliseconds())
		timer := time.NewTimer(secretaryModelRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}
