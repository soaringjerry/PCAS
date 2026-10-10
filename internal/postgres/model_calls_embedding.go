package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// embeddingCalls records embedding invocations in the call journal.
// It stores counts and accounting only. Input texts and vectors stay with the caller.
type embeddingCalls struct{ store *Store }

func (a embeddingCalls) Begin(ctx context.Context, r modelcall.EmbeddingRequest) (modelcall.EmbeddingCall, error) {
	call := modelcall.EmbeddingCall{InvocationID: memory.NewID()}
	bytes := 0
	for _, text := range r.Texts {
		bytes += len(text)
	}
	refs := r.Refs
	if refs == nil {
		refs = []memory.Ref{}
	}
	manifest := asJSON(map[string]any{"version": "embedding-input-v1", "inputs": len(r.Texts), "bytes": bytes, "memoryRefs": refs})
	var err error
	call.Reservation, err = a.store.reserveModelCostID(ctx, r.OwnerID, r.Estimate, r.Job, func(ctx context.Context, tx pgx.Tx, reservation string) error {
		_, err := tx.Exec(ctx, `WITH t AS (SELECT clock_timestamp() AS at)
 INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,causation_id,attempt_number,attempt_limit,function_name,stage,provider_id,model,input_manifest,required_capabilities,actual_mode,outcome,reservation_id,accounting_state,created_at,started_at,updated_at)
 SELECT $1,$2,$3,$4,$5,1,1,$6,$7,$8,$9,$10,'["embedding"]','{"operation":"embedding"}','started',$11,'reserved',t.at,t.at,t.at FROM t`,
			string(r.OwnerID), string(call.InvocationID), string(r.ExecutionID), string(r.RootExecutionID), nullString(string(r.CausationID)), r.Function, r.Stage, r.Provider.ID, r.Provider.Model, manifest, reservation)
		return err
	})
	return call, err
}

// A known outcome settles the reservation to the reported cost. An unknown
// outcome keeps the reservation, because the provider can have charged the call.
func (a embeddingCalls) Finish(ctx context.Context, r modelcall.EmbeddingRequest, call modelcall.EmbeddingCall, usage ai.Result, callErr error) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	outcome, code, accounting := "returned", "", "settled"
	if callErr != nil {
		outcome, code = "failed", "model_call_failed"
		if errors.Is(callErr, memory.ErrUnavailable) {
			code = "provider_unavailable"
		}
		if modelcall.OutcomeUnknown(callErr) {
			outcome, code, accounting = "unknown", modelcall.ErrOutcomeUnknown.Error(), "held"
		}
	}
	return pgx.BeginFunc(persist, a.store.pool, func(tx pgx.Tx) error {
		var usageID any
		var reservation any = call.Reservation
		switch {
		case outcome == "unknown":
		case code == "provider_unavailable":
			// The provider received nothing. Remove the reservation so that a
			// later attempt of the same job is not refused as a second paid call.
			accounting, reservation = "not_reserved", nil
			if _, err := tx.Exec(persist, "DELETE FROM background_usage WHERE owner_id=$1 AND id=$2", string(r.OwnerID), call.Reservation); err != nil {
				return err
			}
		default:
			if usage.InputTokens > 0 {
				usageID = call.Reservation
				record := modelUsage{OwnerID: r.OwnerID, ID: memory.ID(call.Reservation), Purpose: r.Function, AgentID: r.Provider.ID, Model: r.Provider.Model, DurationMS: usage.DurationMS, InputTokens: usage.InputTokens, InputEstimated: usage.InputEstimated, Cost: usage.Cost, CostEstimated: usage.CostEstimated, MemoryRefs: r.Refs}
				if r.Job != nil {
					record.JobID = string(r.Job.ID)
				}
				if err := recordUsageTx(persist, tx, record); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(persist, "UPDATE background_usage SET reserved_cost=$3 WHERE owner_id=$1 AND id=$2", string(r.OwnerID), call.Reservation, usage.Cost); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(persist, `UPDATE model_calls SET outcome=$3,error_code=$4,accounting_state=$5,usage_id=$6,reservation_id=$7,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2 AND outcome='started'`,
			string(r.OwnerID), string(call.InvocationID), outcome, code, accounting, usageID, reservation)
		if err == nil && tag.RowsAffected() != 1 {
			return memory.ErrConflict
		}
		return err
	})
}

// A started call that outlives every provider timeout was interrupted with its
// process. The longest provider request lasts three minutes. Its outcome stays
// unknown and its reservation stays held.
func (a embeddingCalls) recordInterrupted(ctx context.Context) error {
	_, err := a.store.pool.Exec(ctx, `UPDATE model_calls SET outcome='unknown',error_code=$1,accounting_state='held',finished_at=clock_timestamp(),updated_at=clock_timestamp(),actual_mode=actual_mode||'{"interruptedReason":"process_interrupted"}'::jsonb
 WHERE input_manifest->>'version'='embedding-input-v1' AND outcome='started' AND started_at<clock_timestamp()-interval '10 minutes'`, modelcall.ErrOutcomeUnknown.Error())
	return err
}
