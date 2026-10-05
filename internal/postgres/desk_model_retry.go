package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Accounting failures abort the result transaction, as they did before retries.
type secretaryAccountingError struct{ error }

func (s *Store) generateSecretaryModelWithRetry(workCtx, persistCtx context.Context, scope memory.Scope, agentID, prompt string, verify func(context.Context) error) (ai.Result, string, error) {
	stage := "model"
	var accountingErr error
	attempt := 0
	result, err := retrySecretaryModel(workCtx, func(callCtx context.Context) (ai.Result, error) {
		attempt++
		if attempt > 1 {
			// Never resubmit context whose versions or access changed in the wait.
			stage = "verify"
			if err := verify(callCtx); err != nil {
				return ai.Result{}, err
			}
		}
		stage = "budget"
		p, _ := s.models.Get(agentID)
		reservationID, err := s.reserveModelCostID(callCtx, scope.OwnerID, p.Reserve(secretaryInstructions+prompt), nil)
		if err != nil {
			return ai.Result{}, err
		}
		stage = "model"
		result, err := s.models.GenerateWithSearchSchema(callCtx, agentID, secretaryInstructions, prompt, secretaryOutputSchema)
		// HTTP providers may hide cancellation behind an unreachable error.
		if err != nil && callCtx.Err() != nil {
			err = callCtx.Err()
		}
		cost := result.Cost
		if err != nil && strings.TrimSpace(result.Text) == "" {
			cost = 0
		}
		accountingErr = s.settleModelCost(persistCtx, scope.OwnerID, reservationID, cost)
		if accountingErr != nil {
			return result, accountingErr
		}
		return result, err
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
