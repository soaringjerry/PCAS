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
	return a.store.settleModelCost(ctx, request.OwnerID, saved.Reservation, saved.Cost)
}

func (a backgroundCalls) Prepare(ctx context.Context, request modelcall.Request, provider ai.Provider) (memory.ID, error) {
	id := memory.NewID()
	err := pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		// Validate the existing lease before adding call metadata. No transaction
		// stays open across the provider call.
		if err := lockJob(ctx, tx, request.Policy.(worker.Job)); err != nil {
			return err
		}
		var unfinished memory.ID
		err := tx.QueryRow(ctx, `SELECT id::text FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND
 (outcome IN ('prepared','started','unknown') OR accounting_state IN ('reserved','pending','failed')) FOR UPDATE`, string(request.OwnerID), string(request.ExecutionID), request.Stage).Scan(&unfinished)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE model_calls SET outcome='unknown',error_code='provider_outcome_unknown',updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(unfinished))
			if err != nil {
				return err
			}
			id = ""
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		refs := request.Refs
		if refs == nil {
			refs = []memory.Ref{}
		}
		manifest := map[string]any{"version": "background-input-v1", "memoryRefs": refs, "inputHash": fmt.Sprintf("%x", sha256.Sum256(request.Prompt)), "inputBytes": len(request.Prompt), "coverage": "legacy_context_builder", "gaps": []string{"source_manifest", "selection_coverage", "upstream_root_and_cause"}}
		_, err = tx.Exec(ctx, `INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,causation_id,function_name,stage,provider_id,model,prompt_name,instruction_hash,context_builder_version,input_manifest,required_capabilities,actual_mode,outcome,accounting_state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'legacy-background-v1',$12,'["text_generation"]','{"output":"text","search":false,"schema":"none"}','prepared','not_reserved')`, string(request.OwnerID), string(id), string(request.ExecutionID), string(request.RootExecutionID), nullString(string(request.CausationID)), request.Function, request.Stage, provider.ID, provider.Model, request.Instructions.Name(), request.Instructions.Hash(), asJSON(manifest))
		return err
	})
	if err == nil && id == "" {
		return "", modelcall.ErrOutcomeUnknown
	}
	return id, err
}

func (a backgroundCalls) Start(ctx context.Context, request modelcall.Request, saved *modelcall.PaidResult) error {
	tag, err := a.store.pool.Exec(ctx, `UPDATE model_calls SET reservation_id=$3,outcome='started',started_at=clock_timestamp(),updated_at=clock_timestamp(),accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$4::double precision) WHERE owner_id=$1 AND id=$2 AND outcome='prepared'`, string(request.OwnerID), string(saved.InvocationID), saved.Reservation, saved.ReservedCost)
	if err == nil && tag.RowsAffected() != 1 {
		return modelcall.ErrOutcomeUnknown
	}
	return err
}

func (a backgroundCalls) ReservationFailed(ctx context.Context, request modelcall.Request, id memory.ID, cause error) error {
	code := "reservation_failed"
	var jobErr *worker.JobError
	if errors.As(cause, &jobErr) {
		code = jobErr.Code
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.store.pool.Exec(persist, `UPDATE model_calls SET outcome='failed',error_code=$3,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2 AND outcome='prepared'`, string(request.OwnerID), string(id), code)
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
			_, err := tx.Exec(persist, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs,input_estimated,output_estimated,cost_estimated,duration_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(job_id) DO NOTHING`, string(request.OwnerID), string(request.ExecutionID), request.Function, saved.Prompt, saved.Output, saved.Reservation, saved.Provider, saved.Model, saved.InputTokens, saved.OutputTokens, saved.Cost, asJSON(saved.Refs), saved.InputEstimated, saved.OutputEstimated, saved.CostEstimated, saved.DurationMS)
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
			_, err = tx.Exec(persist, `UPDATE model_calls SET outcome=$3,error_code=$4,finished_at=coalesce(finished_at,clock_timestamp()),updated_at=clock_timestamp(),usage_id=$5,result_receipt=jsonb_build_object('kind','background_model_results','jobId',$6::text),accounting_state=CASE WHEN accounting_state='settled' THEN 'settled' ELSE 'pending' END WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), outcome, saved.CallErrorCode, usage, string(request.ExecutionID))
			return err
		})
		cancel()
		if err == nil {
			a.store.pendingPaid.Delete(request.ExecutionID)
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
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.store.pool.Exec(persist, `UPDATE model_calls SET accounting_state=$3,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(request.OwnerID), string(saved.InvocationID), state)
	return err
}

func (a backgroundCalls) Forget(ctx context.Context, request modelcall.Request) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	_, err := a.store.pool.Exec(persist, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2", string(request.OwnerID), string(request.ExecutionID))
	return err
}
