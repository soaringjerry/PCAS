// Package modelcall owns provider invocation and paid-result recovery.
// Business workflows own selection, application, and retry policy.
package modelcall

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

var (
	ErrNotConfigured           = errors.New("provider_not_configured")
	ErrNotAvailable            = errors.New("provider_unavailable")
	ErrOutcomeUnknown          = errors.New("provider_outcome_unknown")
	ErrRetryExhausted          = errors.New("provider_recovery_exhausted")
	ErrRecoveryBudgetExhausted = errors.New("provider_recovery_budget_exhausted")
	ErrNotApplicable           = errors.New("paid_result_not_applicable")
)

// Request separates execution identity from an individual paid invocation.
// Policy is an opaque adapter value; the gateway does not interpret queue rules.
type Request struct {
	OwnerID               memory.ID
	ExecutionID           memory.ID
	RootExecutionID       memory.ID
	CausationID           memory.ID
	Function              string
	Stage                 string
	ProviderID            string
	Instructions          prompts.Definition
	Schema                prompts.Schema
	Search                bool
	ContextBuilderVersion string
	Prompt                json.RawMessage
	Refs                  []memory.Ref
	Policy                any
}

func (r Request) RequiredCapabilities() []string {
	capabilities := []string{"text_generation"}
	if r.Schema.Name() != "" {
		capabilities = append(capabilities, "output_schema")
	}
	if r.Search {
		capabilities = append(capabilities, "web_search")
	}
	return capabilities
}

// PaidResult retains the original input and billing identity on every retry.
// Legacy receipts can lack InvocationID; do not invent historical metadata.
type PaidResult struct {
	InvocationID    memory.ID       `json:"invocationId,omitempty"`
	CallErrorCode   string          `json:"callErrorCode,omitempty"`
	FailureDetails  *FailureDetails `json:"failureDetails,omitempty"`
	NotApplicable   bool            `json:"notApplicable,omitempty"`
	ReservedCost    float64         `json:"reservedCost,omitempty"`
	Searches        []string        `json:"searches,omitempty"`
	DurationMS      *int64
	Prompt          json.RawMessage
	Output          string
	Reservation     string
	Provider        string
	Model           string
	InputTokens     int
	OutputTokens    int
	InputEstimated  bool
	OutputEstimated bool
	CostEstimated   bool
	Cost            float64
	Refs            []memory.Ref
}

type Providers interface {
	Get(string) (ai.Provider, bool)
	ExtractionID() string
	Available(string) bool
	CheckGeneration(ai.Provider, ai.GenerationMode) error
	GenerateProvider(context.Context, ai.Provider, string, string, ...ai.GenerationMode) (ai.Result, error)
}

// Reservation describes the amount actually held. An admitted execution can
// already own a reservation whose amount differs from the current estimate.
type Reservation struct {
	ID   string
	Cost float64
}

type Accounting interface {
	Reserve(context.Context, Request, float64) (Reservation, error)
	Record(context.Context, Request, *PaidResult) error
	Settle(context.Context, Request, *PaidResult) error
}

type Results interface {
	Load(context.Context, Request) (*PaidResult, error)
	// Save atomically stores the result and its returned/failed/unknown status.
	// It must retain a pending result in this process if persistence fails.
	Save(context.Context, Request, *PaidResult) error
	Forget(context.Context, Request, *PaidResult) error
}

type Journal interface {
	Prepare(context.Context, Request, ai.Provider) (memory.ID, error)
	Start(context.Context, Request, *PaidResult) error
	PreparationFailed(context.Context, Request, memory.ID, error) error
	AccountingState(context.Context, Request, *PaidResult, string) error
}

type Gateway struct {
	providers  Providers
	accounting Accounting
	results    Results
	journal    Journal
}

func New(providers Providers, accounting Accounting, results Results, journal Journal) *Gateway {
	return &Gateway{providers: providers, accounting: accounting, results: results, journal: journal}
}

// Failure gives the calling workflow the facts needed for its existing policy.
type Failure struct {
	Code         string
	InvocationID memory.ID
	Details      *FailureDetails
	ReservedCost float64
	Reservation  string
}

func (e *Failure) Error() string { return e.Code }

func (e *Failure) Is(target error) bool {
	return target == ErrOutcomeUnknown && e.Code == ErrOutcomeUnknown.Error()
}

func (e *Failure) Unwrap() error { return e.Details.cause() }

// PersistenceError identifies an unsaved result or incomplete accounting.
// Callers must not replace the paid invocation to repair this failure.
type PersistenceError struct{ Err error }

func (e *PersistenceError) Error() string { return e.Err.Error() }
func (e *PersistenceError) Unwrap() error { return e.Err }

func (g *Gateway) Call(ctx context.Context, request Request) (*PaidResult, error) {
	if request.OwnerID == "" || request.ExecutionID == "" || request.RootExecutionID == "" || request.Function == "" || request.Stage == "" || request.Instructions.Name() == "" {
		return nil, memory.ErrInvalid
	}
	saved, err := g.results.Load(ctx, request)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		if g.providers == nil {
			return nil, ErrNotConfigured
		}
		providerID := request.ProviderID
		if providerID == "" {
			providerID = g.providers.ExtractionID()
		}
		provider, ok := g.providers.Get(providerID)
		if !ok {
			return nil, ErrNotConfigured
		}
		if !g.providers.Available(provider.ID) {
			return nil, ErrNotAvailable
		}
		invocation, err := g.journal.Prepare(ctx, request, provider)
		if err != nil {
			return nil, err
		}
		mode := ai.GenerationMode{Search: request.Search, Schema: request.Schema.Bytes()}
		if err := g.providers.CheckGeneration(provider, mode); err != nil {
			return nil, errors.Join(err, g.journal.PreparationFailed(ctx, request, invocation, err))
		}
		modelPrompt := string(request.Prompt)
		var wrapped struct {
			RawPrompt string `json:"rawPrompt"`
		}
		if json.Unmarshal(request.Prompt, &wrapped) == nil && wrapped.RawPrompt != "" {
			modelPrompt = wrapped.RawPrompt
		}
		saved = &PaidResult{InvocationID: invocation, Prompt: request.Prompt, Provider: provider.ID, Model: provider.Model, Refs: request.Refs, ReservedCost: provider.Reserve(request.Instructions.Text() + modelPrompt)}
		reservation, err := g.accounting.Reserve(ctx, request, saved.ReservedCost)
		if err != nil {
			if journalErr := g.journal.PreparationFailed(ctx, request, invocation, err); journalErr != nil {
				return nil, errors.Join(err, journalErr)
			}
			return nil, err
		}
		saved.Reservation = reservation.ID
		saved.ReservedCost = reservation.Cost
		if err := g.journal.Start(ctx, request, saved); err != nil {
			// No provider call occurred. Release the reservation, without masking
			// a failed lifecycle write or starting an unrecorded invocation.
			journalErr := g.journal.PreparationFailed(ctx, request, invocation, err)
			settleErr := g.accounting.Settle(ctx, request, saved)
			state := "settled"
			if settleErr != nil {
				state = "failed"
			}
			return nil, errors.Join(err, journalErr, settleErr, g.journal.AccountingState(ctx, request, saved, state))
		}
		result, callErr := g.providers.GenerateProvider(ctx, provider, request.Instructions.Text(), modelPrompt, mode)
		saved.Output, saved.DurationMS = result.Text, result.DurationMS
		saved.Searches = result.Searches
		saved.InputTokens, saved.OutputTokens = result.InputTokens, result.OutputTokens
		saved.InputEstimated, saved.OutputEstimated, saved.CostEstimated = result.InputEstimated, result.OutputEstimated, result.CostEstimated
		saved.Cost = result.Cost
		if callErr != nil {
			saved.FailureDetails = failureDetails(callErr)
			saved.CallErrorCode = "model_call_failed"
			if errors.Is(callErr, memory.ErrUnavailable) {
				saved.CallErrorCode = "provider_unavailable"
			}
			if outcomeUnknown(callErr) {
				saved.CallErrorCode = ErrOutcomeUnknown.Error()
			}
		}
	}
	if err := g.results.Save(ctx, request, saved); err != nil {
		return nil, &PersistenceError{Err: err}
	}
	if err := g.finishAccounting(ctx, request, saved); err != nil {
		return nil, &PersistenceError{Err: err}
	}
	if saved.CallErrorCode != "" {
		if err := g.results.Forget(ctx, request, saved); err != nil {
			return nil, err
		}
		return nil, &Failure{Code: saved.CallErrorCode, InvocationID: saved.InvocationID, Details: saved.FailureDetails, ReservedCost: saved.ReservedCost, Reservation: saved.Reservation}
	}
	if saved.NotApplicable {
		return nil, ErrNotApplicable
	}
	return saved, nil
}

// RecoverAccounting finishes a verified stored receipt. It never selects a
// provider, reserves a new budget, or submits a model call. Storage supplies the
// original invocation and verifies its durable owner and billing links first.
func (g *Gateway) RecoverAccounting(ctx context.Context, request Request, saved *PaidResult) error {
	if request.OwnerID == "" || request.ExecutionID == "" || saved == nil || saved.InvocationID == "" || saved.Reservation == "" || saved.Provider == "" || saved.Model == "" {
		return memory.ErrInvalid
	}
	return g.finishAccounting(ctx, request, saved)
}

func (g *Gateway) finishAccounting(ctx context.Context, request Request, saved *PaidResult) error {
	// An unavailable adapter made no invocation. All other attempts retain
	// returned or partial usage even if their output cannot be applied.
	if saved.CallErrorCode != "provider_unavailable" {
		if err := g.accounting.Record(ctx, request, saved); err != nil {
			return g.accountingError(ctx, request, saved, err)
		}
	}
	if err := g.accounting.Settle(ctx, request, saved); err != nil {
		return g.accountingError(ctx, request, saved, err)
	}
	return g.journal.AccountingState(ctx, request, saved, "settled")
}

func outcomeUnknown(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var providerErr *ai.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.OutcomeUnknown
	}
	var codexErr *ai.CodexError
	if errors.As(err, &codexErr) {
		switch codexErr.Category {
		case "transport_error", "timeout", "canceled", "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts", "event_buffer_exceeded", "unknown", "other":
			return true
		}
	}
	return false
}

func (g *Gateway) accountingError(ctx context.Context, request Request, result *PaidResult, err error) error {
	return errors.Join(err, g.journal.AccountingState(ctx, request, result, "failed"))
}

// PersistenceTimeout keeps accounting independent of caller cancellation.
// It retains the existing five-second persistence boundary.
const PersistenceTimeout = 5 * time.Second
