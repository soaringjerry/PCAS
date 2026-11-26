package callstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usagelog"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
)

// Journal is the single storage and accounting adapter for model_calls,
// reservations, and paid results across background jobs and interactive
// executions. Business selection, retry policy, and result application stay
// with callers.
type Journal struct{ Store *Calls }

type preparedCall struct {
	id             memory.ID
	retryOf        memory.ID
	attempt        int
	limit          int
	builderVersion string
	manifest       map[string]any
	mode           map[string]any
}

func insertPreparedCallTx(ctx context.Context, tx pgx.Tx, r modelcall.Request, provider ai.Provider, p preparedCall) error {
	_, err := tx.Exec(ctx, `INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,causation_id,retry_of_id,attempt_number,attempt_limit,function_name,stage,provider_id,model,prompt_name,instruction_hash,schema_name,schema_hash,context_builder_version,input_manifest,required_capabilities,actual_mode,outcome,accounting_state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'prepared','not_reserved')`,
		string(r.OwnerID), string(p.id), string(r.ExecutionID), string(r.RootExecutionID),
		kernel.NullString(string(r.CausationID)), kernel.NullString(string(p.retryOf)),
		p.attempt, p.limit, r.Function, r.Stage, provider.ID, provider.Model,
		r.Instructions.Name(), r.Instructions.Hash(),
		kernel.NullString(r.Schema.Name()), kernel.NullString(r.Schema.Hash()),
		p.builderVersion, kernel.JSON(p.manifest), kernel.JSON(r.RequiredCapabilities()), kernel.JSON(p.mode))
	return err
}

func startModePatch(r modelcall.Request) map[string]any {
	mode := map[string]any{}
	if r.Search || r.Schema.Name() != "" || len(r.Unsupported) > 0 {
		mode["search"] = r.Search
	}
	if len(r.Unsupported) > 0 {
		// The provider lacks these capabilities. The caller validates the
		// ordinary output. Record the mode that was actually submitted.
		mode["unsupportedFallback"] = r.Unsupported
		for _, capability := range r.Unsupported {
			if capability == "web_search" {
				mode["search"] = false
			}
		}
	}
	if r.Schema.Name() != "" {
		mode["schema"] = r.Schema.Name()
		mode["output"] = "structured"
	}
	return mode
}

func classifyCallOutcome(saved *modelcall.PaidResult) (outcome string, usageComplete bool, providerUnavailable bool) {
	outcome = "returned"
	if saved.CallErrorCode != "" {
		outcome = "failed"
	}
	if saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
		outcome = "unknown"
	}
	return outcome, outcome != "unknown", saved.CallErrorCode == "provider_unavailable"
}

func settleCost(saved *modelcall.PaidResult, held float64) float64 {
	cost := saved.Cost
	if saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
		// Partial usage cannot release the unseen remainder of an interrupted
		// call. Keep its original estimate in the existing daily budget.
		cost = max(cost, held)
	}
	return cost
}

func (a Journal) saveWithRetry(ctx context.Context, writeTx func(persist context.Context, tx pgx.Tx) error) error {
	for {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		err := pgx.BeginFunc(persist, a.Store.Pool, func(tx pgx.Tx) error {
			return writeTx(persist, tx)
		})
		cancel()
		if err == nil || errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrInvalid) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}

// Queue and action owners submit recovery changes to the journal adapter.
// Their existing transaction keeps the job, receipt, and metadata atomic.
func (a Journal) AuthorizeRecoveryTx(ctx context.Context, tx pgx.Tx, owner, execution memory.ID, requestID string) error {
	_, err := tx.Exec(ctx, `UPDATE model_calls SET recovery_state='replaced',recovery_reason='explicit_user_retry',actual_mode=actual_mode||jsonb_build_object('recoveryRequestId',$3::text,'previousRecoveryReason',recovery_reason),updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND recovery_state IN ('exhausted','budget_exhausted')`, string(owner), string(execution), requestID)
	return err
}

func (a Journal) ExhaustRecoveryTx(ctx context.Context, tx pgx.Tx, job worker.Job) error {
	_, err := tx.Exec(ctx, `UPDATE model_calls SET outcome=CASE WHEN outcome='prepared' THEN 'failed' ELSE 'unknown' END,error_code=CASE WHEN outcome='prepared' THEN 'provider_not_started' ELSE 'provider_outcome_unknown' END,finished_at=coalesce(finished_at,clock_timestamp()),recovery_state='exhausted',recovery_reason='queue_attempts_exhausted',accounting_state=CASE WHEN reservation_id IS NOT NULL THEN 'held' ELSE accounting_state END,updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND recovery_state='active' AND outcome IN ('prepared','started','unknown')`, string(job.OwnerID), string(job.ID))
	return err
}

func (a Journal) Load(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error) {
	if _, ok := request.Policy.(InteractiveCallPolicy); ok {
		return a.loadInteractive(ctx, request)
	}
	return a.Store.PaidModelResult(ctx, request.Policy.(worker.Job))
}

func (a Journal) Prepare(ctx context.Context, request modelcall.Request, provider ai.Provider) (memory.ID, error) {
	if _, ok := request.Policy.(InteractiveCallPolicy); ok {
		return a.prepareInteractive(ctx, request, provider)
	}
	return a.prepareBackground(ctx, request, provider)
}

func (a Journal) prepareBackground(ctx context.Context, request modelcall.Request, provider ai.Provider) (memory.ID, error) {
	id := memory.NewID()
	var blocked error
	err := pgx.BeginFunc(ctx, a.Store.Pool, func(tx pgx.Tx) error {
		// Validate the existing lease before adding call metadata. No transaction
		// stays open across the provider call.
		job := request.Policy.(worker.Job)
		if err := kernel.LockJob(ctx, tx, job); err != nil {
			return err
		}
		var unfinished memory.ID
		var outcome, accounting, lease, recovery, reservation string
		attempt, limit := 1, kernel.MaxAttempts
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
		builderVersion := request.ContextBuilderVersion
		if builderVersion == "" {
			builderVersion = "legacy-background-v1"
		}
		return insertPreparedCallTx(ctx, tx, request, provider, preparedCall{
			id:             id,
			retryOf:        unfinished,
			attempt:        attempt,
			limit:          limit,
			builderVersion: builderVersion,
			manifest:       manifest,
			mode:           mode,
		})
	})
	if err == nil && id == "" {
		return "", blocked
	}
	return id, err
}

func (a Journal) Reserve(ctx context.Context, request modelcall.Request, estimate float64) (modelcall.Reservation, error) {
	p, interactive := request.Policy.(InteractiveCallPolicy)
	if interactive && p.BudgetOwner == "deputy_run" {
		return a.reserveDeputyRun(ctx, request, estimate)
	}
	var job *worker.Job
	if !interactive {
		j := request.Policy.(worker.Job)
		job = &j
	}
	id, err := a.Store.ReserveModelCostID(ctx, request.OwnerID, estimate, job, func(ctx context.Context, tx pgx.Tx, id string) error {
		// The reservation and its invocation link commit together. A crash
		// before Start must not leave an unlinked budget reservation.
		var tagRows int64
		var err error
		if interactive {
			if err := interactiveFence(ctx, tx, request); err != nil {
				return err
			}
			tag, execErr := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$4,accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$5::double precision),updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND outcome='prepared' AND recovery_state='active' AND actual_mode->>'executionToken'=$6`, string(request.OwnerID), string(request.ExecutionID), request.Stage, id, estimate, p.Token)
			tagRows, err = tag.RowsAffected(), execErr
		} else {
			tag, execErr := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$3,accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$4::double precision),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=(SELECT id FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$5 AND outcome='prepared')`, string(request.OwnerID), string(request.ExecutionID), id, estimate, request.Stage)
			tagRows, err = tag.RowsAffected(), execErr
		}
		if err == nil && tagRows != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
	// The existing foreground budget port uses ErrUnavailable for exhaustion.
	// Keep that cause, and identify the budget failure for gateway callers.
	if interactive && errors.Is(err, memory.ErrUnavailable) {
		return modelcall.Reservation{ID: id, Cost: estimate}, errors.Join(workspace.ErrBudget, err)
	}
	return modelcall.Reservation{ID: id, Cost: estimate}, err
}

func (a Journal) Start(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	p, interactive := request.Policy.(InteractiveCallPolicy)
	mainDeputy := interactive && p.BudgetOwner == "deputy_run"
	var binding string
	if mainDeputy {
		var err error
		binding, err = interactiveBinding(request)
		if err != nil {
			return err
		}
	}
	return pgx.BeginFunc(ctx, a.Store.Pool, func(tx pgx.Tx) error {
		if mainDeputy {
			if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(request.OwnerID)); err != nil {
				return err
			}
		}
		if interactive {
			if err := interactiveFence(ctx, tx, request); err != nil {
				return err
			}
		} else {
			if err := kernel.LockJob(ctx, tx, request.Policy.(worker.Job)); err != nil {
				return err
			}
		}
		mode := startModePatch(request)
		if mainDeputy {
			if err := accessstore.VerifyRunTx(ctx, tx, memory.Scope{OwnerID: request.OwnerID, PrincipalID: p.AgentID}, workspace.Run{AgentID: p.AgentID, ThingID: p.ThingID, ContextVersions: request.Refs}); err != nil {
				return err
			}
			// Input-only rows are not paid results. Commit the original private
			// input and its started journal together before provider submission.
			tag, err := tx.Exec(ctx, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs)
 SELECT owner_id,id,'deputy_input',$4::jsonb,'',reservation_id,provider_id,model,0,0,0,$5::jsonb FROM model_calls
 WHERE owner_id=$1 AND id=$2 AND reservation_id=$3 AND outcome='prepared' AND recovery_state='active' AND input_manifest->>'bindingHash'=$6`, string(request.OwnerID), string(saved.InvocationID), saved.Reservation, request.Prompt, kernel.JSON(request.Refs), binding)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return memory.ErrConflict
			}
			mode["inputPayload"] = "input_saved"
		}
		var tagRows int64
		var err error
		if interactive {
			tag, execErr := tx.Exec(ctx, `UPDATE model_calls SET outcome='started',started_at=clock_timestamp(),updated_at=clock_timestamp(),actual_mode=actual_mode||$4::jsonb WHERE owner_id=$1 AND id=$2 AND reservation_id=$3 AND outcome='prepared' AND recovery_state='active'`, string(request.OwnerID), string(saved.InvocationID), saved.Reservation, kernel.JSON(mode))
			tagRows, err = tag.RowsAffected(), execErr
		} else {
			tag, execErr := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$3,outcome='started',started_at=clock_timestamp(),updated_at=clock_timestamp(),accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$4::double precision)||$5::jsonb WHERE owner_id=$1 AND id=$2 AND outcome='prepared' AND recovery_state='active'`, string(request.OwnerID), string(saved.InvocationID), saved.Reservation, saved.ReservedCost, kernel.JSON(mode))
			tagRows, err = tag.RowsAffected(), execErr
		}
		if err == nil && tagRows != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
}

func (a Journal) PreparationFailed(ctx context.Context, request modelcall.Request, id memory.ID, cause error) error {
	code := "reservation_failed"
	mode := map[string]any{}
	if errors.Is(cause, ai.ErrUnsupportedCapability) {
		code = ai.ErrUnsupportedCapability.Error()
		var capability *ai.CapabilityError
		if errors.As(cause, &capability) {
			mode["unsupportedCapability"] = capability.Capability
		}
	} else if errors.Is(cause, memory.ErrUnavailable) {
		code = modelcall.ErrNotAvailable.Error()
	}
	var jobErr *worker.JobError
	if errors.As(cause, &jobErr) {
		code = jobErr.Code
	}
	budgetExhausted := jobErr != nil && (jobErr.NoAttempt || !jobErr.Until.IsZero())
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	if jobErr != nil && jobErr.NoAttempt {
		// A deferral submits nothing. The queue keeps its reason and next time.
		// A journal row here would report a limit as a failed model call.
		// A recovery replacement keeps its row, because it pauses the chain.
		tag, err := a.Store.Pool.Exec(persist, `DELETE FROM model_calls WHERE owner_id=$1 AND id=$2 AND outcome='prepared' AND retry_of_id IS NULL`, string(request.OwnerID), string(id))
		if err != nil || tag.RowsAffected() == 1 {
			return err
		}
	}
	var recovery string
	err := a.Store.Pool.QueryRow(persist, `UPDATE model_calls SET outcome='failed',error_code=$3,finished_at=clock_timestamp(),updated_at=clock_timestamp(),actual_mode=actual_mode||$5::jsonb,recovery_state=CASE WHEN retry_of_id IS NOT NULL AND $4 THEN 'budget_exhausted' ELSE recovery_state END,recovery_reason=CASE WHEN retry_of_id IS NOT NULL AND $4 THEN $3 ELSE recovery_reason END WHERE owner_id=$1 AND id=$2 AND outcome='prepared' RETURNING recovery_state`, string(request.OwnerID), string(id), code, budgetExhausted, kernel.JSON(mode)).Scan(&recovery)
	if err == nil && recovery == "budget_exhausted" {
		return modelcall.ErrRecoveryBudgetExhausted
	}
	return err
}

// Save retains responses before retryable accounting and application. Commit
// the output and its lifecycle together so restart cannot reinterpret a failed
// response as a successful business result.
func (a Journal) Save(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	if _, ok := request.Policy.(InteractiveCallPolicy); ok {
		return a.saveInteractive(ctx, request, saved)
	}
	a.Store.PendingPaid.Store(request.ExecutionID, saved)
	err := a.saveWithRetry(ctx, func(persist context.Context, tx pgx.Tx) error {
		return a.saveBackgroundTx(persist, tx, request, saved)
	})
	if err == nil {
		a.Store.PendingPaid.CompareAndDelete(request.ExecutionID, saved)
	}
	return err
}

func (a Journal) saveBackgroundTx(persist context.Context, tx pgx.Tx, request modelcall.Request, saved *modelcall.PaidResult) error {
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
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(job_id) DO NOTHING`, string(request.OwnerID), string(request.ExecutionID), request.Function, saved.Prompt, saved.Output, saved.Reservation, saved.Provider, saved.Model, saved.InputTokens, saved.OutputTokens, saved.Cost, kernel.JSON(saved.Refs), saved.InputEstimated, saved.OutputEstimated, saved.CostEstimated, saved.DurationMS)
	}
	if err != nil || saved.InvocationID == "" {
		return err
	}
	outcome, usageComplete, unavailable := classifyCallOutcome(saved)
	var usage any = saved.Reservation
	if unavailable {
		usage = nil
	}
	receipt := map[string]any{"kind": "background_model_results", "jobId": string(request.ExecutionID), "reservationId": saved.Reservation}
	if len(saved.Searches) > 0 {
		receipt["searches"] = saved.Searches
	}
	if !current {
		receipt = map[string]any{"kind": "late_response", "availableForApplication": false}
	}
	_, err = tx.Exec(persist, `UPDATE model_calls SET outcome=$3,error_code=$4,finished_at=clock_timestamp(),updated_at=clock_timestamp(),usage_id=$5,result_receipt=$6,actual_mode=actual_mode||jsonb_build_object('usageComplete',$7::boolean),accounting_state=CASE WHEN accounting_state='settled' THEN 'settled' ELSE 'pending' END WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), outcome, saved.CallErrorCode, usage, kernel.JSON(receipt), usageComplete)
	return err
}

func (a Journal) Record(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	p, interactive := request.Policy.(InteractiveCallPolicy)
	var usage usagelog.ModelUsage
	if interactive {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		defer cancel()
		if err := a.Store.Pool.QueryRow(persist, `SELECT actual_mode->'originalUsage' FROM model_calls WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID)).Scan(&usage); err != nil {
			return err
		}
		ctx = persist
	} else {
		usage.Purpose = request.Function
		usage.JobID = string(request.ExecutionID)
	}
	usage.OwnerID = request.OwnerID
	if usage.ID == "" {
		usage.ID = memory.ID(saved.Reservation)
	}
	usage.AgentID = saved.Provider
	usage.Model = saved.Model
	usage.MemoryRefs = saved.Refs
	usage.DurationMS = saved.DurationMS
	usage.InputTokens = saved.InputTokens
	usage.OutputTokens = saved.OutputTokens
	usage.Cost = saved.Cost
	usage.InputEstimated = saved.InputEstimated
	usage.OutputEstimated = saved.OutputEstimated
	usage.CostEstimated = saved.CostEstimated
	if interactive && p.BudgetOwner == "deputy_run" {
		var snapshot deputy.Snapshot
		if json.Unmarshal(request.Prompt, &snapshot) == nil && snapshot.Run.Kind != "revise" && !snapshot.MemoryReady {
			return a.Store.RecordReturnedUsage(ctx, saved.Output, usage)
		}
	}
	return a.Store.RecordUsage(ctx, usage)
}

func (a Journal) Settle(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	if p, ok := request.Policy.(InteractiveCallPolicy); ok && p.BudgetOwner == "deputy_run" {
		return a.settleDeputyRun(ctx, request, saved)
	}
	return a.Store.SettleModelCost(ctx, request.OwnerID, saved.Reservation, settleCost(saved, saved.ReservedCost))
}

func (a Journal) AccountingState(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult, state string) error {
	if saved.InvocationID == "" {
		return nil
	}
	if state == "settled" && saved.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
		state = "held"
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.Store.Pool.Exec(persist, `UPDATE model_calls SET accounting_state=$3,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), state)
	return err
}

// UnknownRecovery asks the existing worker to use its queue backoff. The
// durable invocation counter also bounds persistent stages and process restarts.
func (a Journal) UnknownRecovery(ctx context.Context, job worker.Job) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	decision := &worker.JobError{Code: modelcall.ErrOutcomeUnknown.Error(), Retry: true}
	err := pgx.BeginFunc(persist, a.Store.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(persist, tx, job); err != nil {
			return err
		}
		var attempt, limit int
		var recovery string
		if err := tx.QueryRow(persist, `SELECT attempt_number,attempt_limit,recovery_state FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND recovery_state<>'replaced' AND (outcome IN ('prepared','started','unknown') OR recovery_state IN ('exhausted','budget_exhausted'))`, string(job.OwnerID), string(job.ID), job.Stage).Scan(&attempt, &limit, &recovery); err != nil {
			return err
		}
		if recovery == "budget_exhausted" {
			decision = &worker.JobError{Code: modelcall.ErrRecoveryBudgetExhausted.Error()}
		} else if attempt >= limit || recovery == "exhausted" || job.Attempts >= kernel.MaxAttempts && !a.Store.Limited(job.Stage) {
			decision = &worker.JobError{Code: modelcall.ErrRetryExhausted.Error()}
			reason := modelcall.ErrRetryExhausted.Error()
			if attempt < limit && job.Attempts >= kernel.MaxAttempts {
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

func (a Journal) Forget(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	// Interactive failures remain recoverable for accounting and caller policy.
	// They cannot become a new invocation unless Prepare validates an explicit retry.
	if _, ok := request.Policy.(InteractiveCallPolicy); ok {
		return nil
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.Store.Pool.Exec(persist, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2 AND reservation_id=$3", string(request.OwnerID), string(request.ExecutionID), saved.Reservation)
	return err
}

// CallPending holds the paid results that are not yet durable. A result
// stays here until its record is written, so that a failed write does not
// lose a paid call.
type CallPending struct {
	PendingPaid             sync.Map
	PendingInteractive      sync.Map
	pendingDeputySelections sync.Map
}

// PendingDeputyRun identifies a leased deputy run whose paid result is still
// held in process memory waiting for durable persistence.
type PendingDeputyRun struct {
	OwnerID memory.ID
	RunID   string
	Origin  string
	AgentID string
	ThingID string
	Revise  bool
}

// PendingDeputyRuns returns the deputy runs that currently have an unsaved
// paid result in process memory so the deputy worker can requeue their leases
// instead of marking them interrupted.
func (c *CallPending) PendingDeputyRuns() []PendingDeputyRun {
	var out []PendingDeputyRun
	c.PendingPaid.Range(func(key, value any) bool {
		var snap deputy.Snapshot
		paid, ok := value.(*PaidModelResult)
		if !ok || json.Unmarshal(paid.Prompt, &snap) != nil || snap.Run.Kind != "revise" {
			return true
		}
		out = append(out, PendingDeputyRun{RunID: string(key.(memory.ID)), Origin: snap.Run.CreatedAt, AgentID: snap.Run.AgentID, ThingID: snap.Run.ThingID, Revise: true})
		return true
	})
	c.PendingInteractive.Range(func(_, value any) bool {
		entry := value.(*PendingInteractiveResult)
		policy := entry.Request.Policy.(InteractiveCallPolicy)
		if policy.BudgetOwner == "deputy_run" {
			out = append(out, PendingDeputyRun{OwnerID: entry.Request.OwnerID, RunID: string(entry.Request.ExecutionID), Origin: policy.Origin})
		}
		return true
	})
	return out
}

// HasPendingInteractive reports whether any interactive response is still
// waiting in process memory to be saved to storage.
func (c *CallPending) HasPendingInteractive() bool {
	pending := false
	c.PendingInteractive.Range(func(_, _ any) bool { pending = true; return false })
	return pending
}
