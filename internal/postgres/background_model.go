package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type paidModelResult = modelcall.PaidResult

func (s *Store) paidModelResult(ctx context.Context, j worker.Job) (*paidModelResult, error) {
	if pending, ok := s.pendingPaid.Load(j.ID); ok {
		saved := pending.(*paidModelResult)
		current := true
		if saved.InvocationID != "" {
			if err := s.pool.QueryRow(ctx, `SELECT recovery_state<>'replaced' FROM model_calls WHERE owner_id=$1 AND id=$2`, string(j.OwnerID), string(saved.InvocationID)).Scan(&current); err != nil {
				return nil, err
			}
		}
		if current {
			return saved, nil
		}
		s.pendingPaid.CompareAndDelete(j.ID, saved)
	}
	r := &paidModelResult{}
	err := s.pool.QueryRow(ctx, `SELECT b.prompt,b.output,b.reservation_id::text,b.provider_id,b.model,b.input_tokens,b.output_tokens,b.cost,b.refs,b.input_estimated,b.output_estimated,b.cost_estimated,b.duration_ms,coalesce(c.id::text,''),coalesce(c.error_code,''),coalesce((c.actual_mode->>'reservationEstimate')::double precision,0),coalesce(c.result_receipt->'searches','[]'::jsonb)
 FROM background_model_results b LEFT JOIN model_calls c ON c.owner_id=b.owner_id AND c.reservation_id=b.reservation_id WHERE b.owner_id=$1 AND b.job_id=$2 AND b.purpose<>'deputy_input'`, string(j.OwnerID), string(j.ID)).Scan(&r.Prompt, &r.Output, &r.Reservation, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens, &r.Cost, &r.Refs, &r.InputEstimated, &r.OutputEstimated, &r.CostEstimated, &r.DurationMS, &r.InvocationID, &r.CallErrorCode, &r.ReservedCost, &r.Searches)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// Temporary adapter for the existing background domain callers. Sol owns its
// removal when those callers pass declared gateway requests directly.
func (s *Store) generatePaid(ctx context.Context, j worker.Job, purpose, instructions string, prompt json.RawMessage, refs []memory.Ref) (*paidModelResult, error) {
	definition, ok := prompts.FromText(instructions)
	if !ok {
		return nil, &worker.JobError{Code: "prompt_not_registered"}
	}
	if s.calls == nil {
		return nil, &worker.JobError{Code: "provider_not_configured"}
	}
	result, err := s.calls.Call(ctx, modelcall.Request{OwnerID: j.OwnerID, ExecutionID: j.ID, RootExecutionID: j.ID, Function: purpose, Stage: j.Stage, Instructions: definition, Prompt: prompt, Refs: refs, Policy: j})
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
		return nil, backgroundCalls{store: s}.UnknownRecovery(ctx, j)
	}
	var failed *modelcall.Failure
	if errors.As(err, &failed) {
		if backgroundHourlyBudgets[backgroundStage(j.Stage)] == 0 {
			if failed.Code == "provider_unavailable" {
				// The gateway settled this invocation before returning its failure.
				if err := s.releaseUnavailableReservation(ctx, j, failed.Reservation); err != nil {
					return nil, err
				}
				return nil, &worker.JobError{Code: "provider_unavailable", Retry: true}
			}
			return nil, &worker.JobError{Code: "model_call_failed", Retry: failed.ReservedCost == 0}
		}
		return nil, &worker.JobError{Code: "model_call_failed", Until: time.Now().Add(retryDelay(j.Attempts))}
	}
	return result, err
}

func discardPaidResultTx(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	_, err := tx.Exec(ctx, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2", string(j.OwnerID), string(j.ID))
	return err
}
