package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// backgroundCalls is the storage/accounting adapter for existing leased jobs.
// Business selection, job retry policy, and result application stay with callers.
type backgroundCalls struct{ store *Store }

// Queue and action owners submit recovery changes to the journal adapter.
// Their existing transaction keeps the job, receipt, and metadata atomic.
func (a backgroundCalls) authorizeRecoveryTx(ctx context.Context, tx pgx.Tx, owner, execution memory.ID, requestID string) error {
	_, err := tx.Exec(ctx, `UPDATE model_calls SET recovery_state='replaced',recovery_reason='explicit_user_retry',actual_mode=actual_mode||jsonb_build_object('recoveryRequestId',$3::text,'previousRecoveryReason',recovery_reason),updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND recovery_state IN ('exhausted','budget_exhausted')`, string(owner), string(execution), requestID)
	return err
}

func (a backgroundCalls) exhaustRecoveryTx(ctx context.Context, tx pgx.Tx, job worker.Job) error {
	_, err := tx.Exec(ctx, `UPDATE model_calls SET outcome=CASE WHEN outcome='prepared' THEN 'failed' ELSE 'unknown' END,error_code=CASE WHEN outcome='prepared' THEN 'provider_not_started' ELSE 'provider_outcome_unknown' END,finished_at=coalesce(finished_at,clock_timestamp()),recovery_state='exhausted',recovery_reason='queue_attempts_exhausted',accounting_state=CASE WHEN reservation_id IS NOT NULL THEN 'held' ELSE accounting_state END,updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND recovery_state='active' AND outcome IN ('prepared','started','unknown')`, string(job.OwnerID), string(job.ID))
	return err
}

func (a backgroundCalls) Load(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error) {
	return a.store.paidModelResult(ctx, request.Policy.(worker.Job))
}

func (a backgroundCalls) Reserve(ctx context.Context, request modelcall.Request, estimate float64) (string, error) {
	j := request.Policy.(worker.Job)
	return a.store.reserveModelCostID(ctx, request.OwnerID, estimate, &j, func(ctx context.Context, tx pgx.Tx, id string) error {
		// The reservation and its invocation link commit together. A crash
		// before Start must not leave an unlinked budget reservation.
		tag, err := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$3,accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$4::double precision),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=(SELECT id FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$5 AND outcome='prepared')`, string(request.OwnerID), string(request.ExecutionID), id, estimate, request.Stage)
		if err == nil && tag.RowsAffected() != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
}

func (a backgroundCalls) Record(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	return a.store.recordUsage(ctx, modelUsage{OwnerID: request.OwnerID, ID: memory.ID(saved.Reservation), Purpose: request.Function, AgentID: saved.Provider, Model: saved.Model, DurationMS: saved.DurationMS, InputTokens: saved.InputTokens, OutputTokens: saved.OutputTokens, InputEstimated: saved.InputEstimated, OutputEstimated: saved.OutputEstimated, CostEstimated: saved.CostEstimated, Cost: saved.Cost, JobID: string(request.ExecutionID), MemoryRefs: saved.Refs})
}

func (a backgroundCalls) Settle(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	cost := saved.Cost
	if saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
		// Partial usage cannot release the unseen remainder of an interrupted
		// call. Keep its original estimate in the existing daily budget.
		cost = max(cost, saved.ReservedCost)
	}
	return a.store.settleModelCost(ctx, request.OwnerID, saved.Reservation, cost)
}

func (a backgroundCalls) Prepare(ctx context.Context, request modelcall.Request, provider ai.Provider) (memory.ID, error) {
	id := memory.NewID()
	var blocked error
	err := pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		// Validate the existing lease before adding call metadata. No transaction
		// stays open across the provider call.
		job := request.Policy.(worker.Job)
		if err := lockJob(ctx, tx, job); err != nil {
			return err
		}
		var unfinished memory.ID
		var outcome, accounting, lease, recovery, reservation string
		attempt, limit := 1, maxAttempts
		err := tx.QueryRow(ctx, `SELECT id::text,outcome,accounting_state,coalesce(actual_mode->>'leaseToken',''),recovery_state,attempt_number,attempt_limit,coalesce(reservation_id::text,'')
 FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND recovery_state<>'replaced' AND
 (outcome IN ('prepared','started','unknown') OR accounting_state IN ('reserved','pending','failed','held') OR (retry_of_id IS NOT NULL AND outcome='failed' AND accounting_state='not_reserved') OR recovery_state IN ('exhausted','budget_exhausted')) FOR UPDATE`, string(request.OwnerID), string(request.ExecutionID), request.Stage).Scan(&unfinished, &outcome, &accounting, &lease, &recovery, &attempt, &limit, &reservation)
		if err == nil {
			// One current lease cannot launch concurrent replacements. Recovery
			// needs the existing queue's next lease and its normal backoff.
			blocked = modelcall.ErrOutcomeUnknown
			if recovery == "exhausted" {
				blocked = modelcall.ErrRetryExhausted
			}
			if recovery == "budget_exhausted" {
				blocked = modelcall.ErrRecoveryBudgetExhausted
			}
			if recovery != "active" || lease == string(job.LeaseToken) || accounting == "pending" || accounting == "failed" || outcome == "returned" {
				id = ""
				return nil
			}
			if outcome == "prepared" || outcome == "failed" {
				// Start validates the lease in its own transaction. An expired
				// prepared call cannot reach the provider, so release its hold.
				if reservation != "" {
					if _, err := tx.Exec(ctx, `UPDATE background_usage SET reserved_cost=0 WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), reservation); err != nil {
						return err
					}
				}
				_, err = tx.Exec(ctx, `UPDATE model_calls SET outcome='failed',error_code=CASE WHEN outcome='failed' THEN error_code ELSE 'provider_not_started' END,finished_at=coalesce(finished_at,clock_timestamp()),accounting_state=CASE WHEN reservation_id IS NULL THEN 'not_reserved' ELSE 'settled' END,recovery_state='replaced',recovery_reason='recovery_preparation_retried',updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(unfinished))
			} else {
				state, reason := "replaced", "unknown_outcome_recovery"
				if attempt >= limit {
					state, reason = "exhausted", modelcall.ErrRetryExhausted.Error()
					blocked = modelcall.ErrRetryExhausted
					id = ""
				}
				_, err = tx.Exec(ctx, `UPDATE model_calls SET outcome='unknown',error_code='provider_outcome_unknown',finished_at=coalesce(finished_at,clock_timestamp()),accounting_state=CASE WHEN reservation_id IS NOT NULL THEN 'held' ELSE accounting_state END,recovery_state=$3,recovery_reason=$4,actual_mode=actual_mode||'{"usageComplete":false}'::jsonb,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(unfinished), state, reason)
				attempt++
			}
			if err != nil || id == "" {
				return err
			}
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if unfinished == "" {
			// An explicit owner retry authorizes a new automatic allowance.
			// Link it to the last paused call without changing historical counts.
			err := tx.QueryRow(ctx, `SELECT c.id::text FROM model_calls c WHERE c.owner_id=$1 AND c.execution_id=$2 AND c.stage=$3 AND c.recovery_reason='explicit_user_retry' AND NOT EXISTS(SELECT 1 FROM model_calls next WHERE next.owner_id=c.owner_id AND next.retry_of_id=c.id) ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, string(request.OwnerID), string(request.ExecutionID), request.Stage).Scan(&unfinished)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		refs := request.Refs
		if refs == nil {
			refs = []memory.Ref{}
		}
		manifest := map[string]any{"version": "background-input-v1", "memoryRefs": refs, "inputHash": fmt.Sprintf("%x", sha256.Sum256(request.Prompt)), "inputBytes": len(request.Prompt), "coverage": "legacy_context_builder", "gaps": []string{"source_manifest", "selection_coverage", "upstream_root_and_cause"}}
		mode := map[string]any{"output": "text", "search": false, "schema": "none", "leaseToken": string(job.LeaseToken), "usageComplete": false}
		_, err = tx.Exec(ctx, `INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,causation_id,function_name,stage,provider_id,model,prompt_name,instruction_hash,context_builder_version,input_manifest,required_capabilities,actual_mode,outcome,accounting_state,retry_of_id,attempt_number,attempt_limit)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'legacy-background-v1',$12,'["text_generation"]',$13,'prepared','not_reserved',$14,$15,$16)`, string(request.OwnerID), string(id), string(request.ExecutionID), string(request.RootExecutionID), nullString(string(request.CausationID)), request.Function, request.Stage, provider.ID, provider.Model, request.Instructions.Name(), request.Instructions.Hash(), asJSON(manifest), asJSON(mode), nullString(string(unfinished)), attempt, limit)
		return err
	})
	if err == nil && id == "" {
		return "", blocked
	}
	return id, err
}

func (a backgroundCalls) Start(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	return pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, request.Policy.(worker.Job)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$3,outcome='started',started_at=clock_timestamp(),updated_at=clock_timestamp(),accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$4::double precision) WHERE owner_id=$1 AND id=$2 AND outcome='prepared' AND recovery_state='active'`, string(request.OwnerID), string(saved.InvocationID), saved.Reservation, saved.ReservedCost)
		if err == nil && tag.RowsAffected() != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
}

func (a backgroundCalls) ReservationFailed(ctx context.Context, request modelcall.Request, id memory.ID, cause error) error {
	code := "reservation_failed"
	var jobErr *worker.JobError
	if errors.As(cause, &jobErr) {
		code = jobErr.Code
	}
	budgetExhausted := jobErr != nil && (jobErr.NoAttempt || !jobErr.Until.IsZero())
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	var recovery string
	err := a.store.pool.QueryRow(persist, `UPDATE model_calls SET outcome='failed',error_code=$3,finished_at=clock_timestamp(),updated_at=clock_timestamp(),recovery_state=CASE WHEN retry_of_id IS NOT NULL AND $4 THEN 'budget_exhausted' ELSE recovery_state END,recovery_reason=CASE WHEN retry_of_id IS NOT NULL AND $4 THEN $3 ELSE recovery_reason END WHERE owner_id=$1 AND id=$2 AND outcome='prepared' RETURNING recovery_state`, string(request.OwnerID), string(id), code, budgetExhausted).Scan(&recovery)
	if err == nil && recovery == "budget_exhausted" {
		return modelcall.ErrRecoveryBudgetExhausted
	}
	return err
}

// Save retains responses before retryable accounting and application. Commit
// the output and its lifecycle together so restart cannot reinterpret a failed
// response as a successful business result.
func (a backgroundCalls) Save(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	a.store.pendingPaid.Store(request.ExecutionID, saved)
	for {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		err := pgx.BeginFunc(persist, a.store.pool, func(tx pgx.Tx) error {
			current := true
			if saved.InvocationID != "" {
				if err := tx.QueryRow(persist, `SELECT recovery_state<>'replaced' FROM model_calls WHERE owner_id=$1 AND id=$2 FOR UPDATE`, string(request.OwnerID), string(saved.InvocationID)).Scan(&current); err != nil {
					return err
				}
			}
			// A reclaimed worker can return late. Keep its call and accounting,
			// but never put its result in the replacement's application slot.
			var err error
			if current {
				_, err = tx.Exec(persist, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs,input_estimated,output_estimated,cost_estimated,duration_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(job_id) DO NOTHING`, string(request.OwnerID), string(request.ExecutionID), request.Function, saved.Prompt, saved.Output, saved.Reservation, saved.Provider, saved.Model, saved.InputTokens, saved.OutputTokens, saved.Cost, asJSON(saved.Refs), saved.InputEstimated, saved.OutputEstimated, saved.CostEstimated, saved.DurationMS)
			}
			if err != nil || saved.InvocationID == "" {
				return err
			}
			outcome := "returned"
			if saved.CallErrorCode != "" {
				outcome = "failed"
			}
			if saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
				outcome = "unknown"
			}
			var usage any = saved.Reservation
			if saved.CallErrorCode == "provider_unavailable" {
				usage = nil
			}
			receipt := map[string]any{"kind": "background_model_results", "jobId": string(request.ExecutionID), "reservationId": saved.Reservation}
			if !current {
				receipt = map[string]any{"kind": "late_response", "availableForApplication": false}
			}
			_, err = tx.Exec(persist, `UPDATE model_calls SET outcome=$3,error_code=$4,finished_at=clock_timestamp(),updated_at=clock_timestamp(),usage_id=$5,result_receipt=$6,actual_mode=actual_mode||jsonb_build_object('usageComplete',$7::boolean),accounting_state=CASE WHEN accounting_state='settled' THEN 'settled' ELSE 'pending' END WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), outcome, saved.CallErrorCode, usage, asJSON(receipt), outcome != "unknown")
			return err
		})
		cancel()
		if err == nil {
			a.store.pendingPaid.CompareAndDelete(request.ExecutionID, saved)
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}

func (a backgroundCalls) AccountingState(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult, state string) error {
	if saved.InvocationID == "" {
		return nil
	}
	if state == "settled" && saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
		state = "held"
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.store.pool.Exec(persist, `UPDATE model_calls SET accounting_state=$3,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), state)
	return err
}

// UnknownRecovery asks the existing worker to use its queue backoff. The
// durable invocation counter also bounds persistent stages and process restarts.
func (a backgroundCalls) UnknownRecovery(ctx context.Context, job worker.Job) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	decision := &worker.JobError{Code: modelcall.ErrOutcomeUnknown.Error(), Retry: true}
	err := pgx.BeginFunc(persist, a.store.pool, func(tx pgx.Tx) error {
		if err := lockJob(persist, tx, job); err != nil {
			return err
		}
		var attempt, limit int
		var recovery string
		if err := tx.QueryRow(persist, `SELECT attempt_number,attempt_limit,recovery_state FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND recovery_state<>'replaced' AND (outcome IN ('prepared','started','unknown') OR recovery_state IN ('exhausted','budget_exhausted'))`, string(job.OwnerID), string(job.ID), job.Stage).Scan(&attempt, &limit, &recovery); err != nil {
			return err
		}
		if recovery == "budget_exhausted" {
			decision = &worker.JobError{Code: modelcall.ErrRecoveryBudgetExhausted.Error()}
		} else if attempt >= limit || recovery == "exhausted" || job.Attempts >= maxAttempts && !persistentBackgroundStage(job.Stage) {
			decision = &worker.JobError{Code: modelcall.ErrRetryExhausted.Error()}
			reason := modelcall.ErrRetryExhausted.Error()
			if attempt < limit && job.Attempts >= maxAttempts {
				reason = "queue_attempts_exhausted"
			}
			_, err := tx.Exec(persist, `UPDATE model_calls SET recovery_state='exhausted',recovery_reason=$4,updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND outcome='unknown' AND recovery_state='active'`, string(job.OwnerID), string(job.ID), job.Stage, reason)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return decision
}

func (a backgroundCalls) Forget(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.store.pool.Exec(persist, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2 AND reservation_id=$3", string(request.OwnerID), string(request.ExecutionID), saved.Reservation)
	return err
}
