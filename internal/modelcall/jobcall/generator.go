// Package jobcall makes the model call of a background job. It owns the
// request of the call and the queue failure that each gateway result becomes.
// Storage keeps the paid results and the job's reservations.
package jobcall

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Store is the storage of the paid results of background jobs.
type Store interface {
	// SavedResult returns the paid result that the job has not applied yet,
	// or nil.
	SavedResult(ctx context.Context, job worker.Job) (*modelcall.PaidResult, error)
	// UnknownRecovery returns the queue failure of a call whose outcome is
	// not known.
	UnknownRecovery(ctx context.Context, job worker.Job) error
	// ReleaseUnavailable removes the reservation of a call that the provider
	// did not accept.
	ReleaseUnavailable(ctx context.Context, job worker.Job, reservation string) error
	// HourlyLimited reports whether the job's stage has an hourly limit.
	HourlyLimited(stage string) bool
	// RetryDelay is the queue's delay before the next attempt.
	RetryDelay(attempts int) time.Duration
}

// Generator submits the model calls of background jobs.
type Generator struct {
	// Calls is nil when no gateway is configured.
	Calls *modelcall.Gateway
	Store Store
}

// HasSavedResult reports whether the job already has a paid result to apply.
func (g Generator) HasSavedResult(ctx context.Context, job worker.Job) (bool, error) {
	saved, err := g.Store.SavedResult(ctx, job)
	return saved != nil, err
}

// GenerateForJob returns the saved input record and the model output. A saved
// result is returned without another call. modelInput is the text that the
// provider receives; when it is empty the provider receives the record.
func (g Generator) GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, error) {
	paid, err := g.Generate(ctx, job, purpose, instructions, modelInput, record, refs)
	if err != nil {
		return nil, "", err
	}
	return paid.Prompt, paid.Output, nil
}

// GenerateWithRefs is GenerateForJob for a caller that also needs the records
// that the saved result names.
func (g Generator) GenerateWithRefs(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, []memory.Ref, error) {
	paid, err := g.Generate(ctx, job, purpose, instructions, "", record, refs)
	if err != nil {
		return nil, "", nil, err
	}
	return paid.Prompt, paid.Output, paid.Refs, nil
}

// Generate submits the job's call and maps each gateway result to the queue's
// failure policy.
func (g Generator) Generate(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (*modelcall.PaidResult, error) {
	if g.Calls == nil {
		return nil, &worker.JobError{Code: "provider_not_configured"}
	}
	result, err := g.Calls.Call(ctx, modelcall.Request{OwnerID: job.OwnerID, ExecutionID: job.ID, RootExecutionID: job.ID, Function: purpose, Stage: job.Stage, Instructions: instructions, Prompt: record, ModelInput: modelInput, Refs: refs, Policy: job})
	if errors.Is(err, modelcall.ErrNotConfigured) {
		return nil, &worker.JobError{Code: "provider_not_configured"}
	}
	if errors.Is(err, modelcall.ErrNotAvailable) {
		return nil, &worker.JobError{Code: "provider_unavailable", Until: time.Now().Add(time.Minute), NoAttempt: true}
	}
	if errors.Is(err, modelcall.ErrRetryExhausted) || errors.Is(err, modelcall.ErrRecoveryBudgetExhausted) {
		code := modelcall.ErrRetryExhausted.Error()
		if errors.Is(err, modelcall.ErrRecoveryBudgetExhausted) {
			code = modelcall.ErrRecoveryBudgetExhausted.Error()
		}
		return nil, &worker.JobError{Code: code}
	}
	if errors.Is(err, modelcall.ErrOutcomeUnknown) {
		return nil, g.Store.UnknownRecovery(ctx, job)
	}
	var failed *modelcall.Failure
	if errors.As(err, &failed) {
		if !g.Store.HourlyLimited(job.Stage) {
			if failed.Code == "provider_unavailable" {
				// The gateway settled this invocation before returning its failure.
				if err := g.Store.ReleaseUnavailable(ctx, job, failed.Reservation); err != nil {
					return nil, err
				}
				return nil, &worker.JobError{Code: "provider_unavailable", Retry: true}
			}
			return nil, &worker.JobError{Code: "model_call_failed", Retry: failed.ReservedCost == 0}
		}
		return nil, &worker.JobError{Code: "model_call_failed", Until: time.Now().Add(g.Store.RetryDelay(job.Attempts))}
	}
	return result, err
}
