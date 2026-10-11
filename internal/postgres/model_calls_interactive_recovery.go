package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// Recovery uses the existing worker's one-item scheduling discipline. Each
// category has at most one item per pass; unselected work remains queued in
// its original cache or journal, with remaining counts returned to the worker.
type interactiveRecoveryStatus struct {
	Results            int
	Accounting         int
	Interrupted        int
	PendingResults     int
	PendingAccounting  int
	PendingInterrupted int
	CountsKnown        bool
}

// This expression only observes existing execution fences. It cannot authorize
// a new execution, replace a call, or extend the caller's original deadline.
const inactiveInteractiveExecutionSQL = `NOT CASE c.input_manifest->>'executionKind'
 WHEN 'secretary' THEN EXISTS(SELECT 1 FROM desk_turn_order t WHERE t.owner_id=c.owner_id AND t.request_id=c.execution_id AND t.creator_id::text=c.actual_mode->>'executionToken' AND encode(t.request_hash,'hex')=c.input_manifest->>'origin' AND t.status='pending' AND t.expires_at>clock_timestamp())
 WHEN 'deputy' THEN EXISTS(SELECT 1 FROM agent_runs r WHERE r.owner_id=c.owner_id AND r.id=c.execution_id AND r.lease_token::text=c.actual_mode->>'executionToken' AND r.document->>'createdAt'=c.input_manifest->>'origin' AND r.agent_id=c.input_manifest->>'agentId' AND r.thing_id::text=c.input_manifest->>'thingId' AND r.status='running' AND r.lease_until>clock_timestamp())
 ELSE true END`

func (s *Store) recoverInteractiveCallsOnce(ctx context.Context) (interactiveRecoveryStatus, error) {
	status := interactiveRecoveryStatus{}
	persist, cancel := context.WithTimeout(ctx, modelcall.PersistenceTimeout)
	defer cancel()
	var exists bool
	if err := s.pool.QueryRow(persist, "SELECT to_regclass('model_calls') IS NOT NULL").Scan(&exists); err != nil {
		return status, err
	}
	if !exists {
		status.CountsKnown = true
		return status, nil
	}
	adapter := interactiveCalls{store: s}
	var failures []error
	var oldest *pendingInteractiveResult
	s.pendingInteractive.Range(func(_, value any) bool {
		entry := value.(*pendingInteractiveResult)
		if oldest == nil || entry.attemptedAt.Before(oldest.attemptedAt) {
			oldest = entry
		}
		return true
	})
	if oldest != nil {
		paid := cloneInteractivePaid(oldest.paid)
		err := adapter.Save(persist, oldest.request, paid)
		if err == nil {
			err = s.calls.RecoverAccounting(persist, oldest.request, paid)
		}
		if err != nil {
			failures = append(failures, err)
		} else {
			status.Results = 1
		}
	}
	if persist.Err() == nil {
		recovered, err := adapter.recoverAccountingReceipt(persist)
		if err != nil {
			failures = append(failures, err)
		} else if recovered {
			status.Accounting = 1
		}
	}
	if persist.Err() == nil {
		interrupted, err := adapter.recordInterruptedExecution(persist)
		if err != nil {
			failures = append(failures, err)
		} else if interrupted {
			status.Interrupted = 1
		}
	}
	if persist.Err() == nil {
		if err := (embeddingCalls{store: s}).recordInterrupted(persist); err != nil {
			failures = append(failures, err)
		}
	}
	s.pendingInteractive.Range(func(_, _ any) bool { status.PendingResults++; return true })
	if persist.Err() == nil {
		err := s.pool.QueryRow(persist, `SELECT
 count(*) FILTER(WHERE accounting_state IN ('pending','failed','reserved') AND result_receipt->'billing' IS NOT NULL AND result_receipt->'billing'<>'null'::jsonb),
 count(*) FILTER(WHERE outcome IN ('prepared','started') AND (`+inactiveInteractiveExecutionSQL+`))
 FROM model_calls c WHERE input_manifest->>'version'='interactive-input-v1'`).Scan(&status.PendingAccounting, &status.PendingInterrupted)
		if err != nil {
			failures = append(failures, err)
		} else {
			status.CountsKnown = true
		}
	}
	if persist.Err() != nil {
		failures = append(failures, persist.Err())
	}
	return status, errors.Join(failures...)
}

// Billing recovery needs only the original immutable receipt. Source deletion
// can remove every private body without preventing original usage settlement.
func (a interactiveCalls) recoverAccountingReceipt(ctx context.Context) (bool, error) {
	var row interactiveCallRow
	err := a.store.pool.QueryRow(ctx, `SELECT to_jsonb(c) FROM model_calls c WHERE input_manifest->>'version'='interactive-input-v1' AND accounting_state IN ('pending','failed','reserved') AND result_receipt->'billing' IS NOT NULL AND result_receipt->'billing'<>'null'::jsonb ORDER BY updated_at,id LIMIT 1`).Scan(&row)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	paid := row.Receipt.Billing
	expectedUsage := row.Mode.Usage.ID
	if expectedUsage == "" {
		expectedUsage = memory.ID(row.Reservation)
	}
	valid := paid != nil && row.OwnerID.Valid() && row.ExecutionID.Valid() && row.RootExecutionID.Valid() && row.Receipt.Kind == "interactive_invocation" && row.Receipt.InvocationID == row.ID && paid.InvocationID == row.ID && paid.Reservation == row.Reservation && paid.Provider == row.Provider && paid.Model == row.Model && paid.CallErrorCode == row.ErrorCode && bytes.Equal(asJSON(uniqueRefs(paid.Refs)), asJSON(uniqueRefs(row.Manifest.Refs)))
	if paid != nil && paid.CallErrorCode != "provider_unavailable" {
		valid = valid && row.UsageID == expectedUsage
	}
	if !valid {
		_, markErr := a.store.pool.Exec(ctx, `UPDATE model_calls SET accounting_state='failed',actual_mode=actual_mode||'{"accountingRecoveryError":"invalid_receipt"}'::jsonb,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(row.OwnerID), string(row.ID))
		return false, errors.Join(fmt.Errorf("interactive_billing_receipt_invalid: %w", memory.ErrConflict), markErr)
	}
	request := modelcall.Request{OwnerID: row.OwnerID, ExecutionID: row.ExecutionID, RootExecutionID: row.RootExecutionID, CausationID: row.CausationID, Function: row.Function, Stage: row.Stage, ProviderID: row.Provider, Policy: interactiveCallPolicy{Kind: row.Manifest.Kind, Origin: row.Manifest.Origin, BudgetOwner: row.Manifest.BudgetOwner, Usage: row.Mode.Usage}}
	if err := a.store.calls.RecoverAccounting(ctx, request, paid); err != nil {
		return false, err
	}
	_, err = a.store.pool.Exec(ctx, `UPDATE model_calls c SET result_receipt=jsonb_set(result_receipt,'{availableForApplication}','false'::jsonb),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2 AND input_manifest->>'budgetOwner' IS DISTINCT FROM 'deputy_run' AND (`+inactiveInteractiveExecutionSQL+`)`, string(row.OwnerID), string(row.ID))
	return err == nil, err
}

// An expired execution cannot start its prepared call. A started call has an
// unknown outcome until an actual response arrives. Neither state is retried.
func (a interactiveCalls) recordInterruptedExecution(ctx context.Context) (bool, error) {
	recorded := false
	var owner memory.ID
	err := a.store.pool.QueryRow(ctx, `SELECT owner_id FROM model_calls c WHERE input_manifest->>'version'='interactive-input-v1' AND outcome IN ('prepared','started') AND (`+inactiveInteractiveExecutionSQL+`) ORDER BY updated_at,id LIMIT 1`).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		// Data-owner writes lock the owner before the journal. Keep that order
		// when releasing an admitted deputy reservation after lease expiration.
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(owner)); err != nil {
			return err
		}
		var row interactiveCallRow
		err := tx.QueryRow(ctx, `SELECT to_jsonb(c) FROM model_calls c WHERE owner_id=$1 AND input_manifest->>'version'='interactive-input-v1' AND outcome IN ('prepared','started') AND (`+inactiveInteractiveExecutionSQL+`) ORDER BY updated_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, string(owner)).Scan(&row)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		outcome, code, accounting := "unknown", modelcall.ErrOutcomeUnknown.Error(), "not_reserved"
		if row.Reservation != "" {
			accounting = "held"
		}
		if row.Outcome == "prepared" {
			outcome, code = "failed", "provider_not_started"
			if row.Reservation != "" {
				var err error
				if row.Manifest.BudgetOwner == "deputy_run" {
					request := modelcall.Request{OwnerID: row.OwnerID, ExecutionID: row.ExecutionID, Policy: interactiveCallPolicy{Origin: row.Manifest.Origin}}
					paid := &modelcall.PaidResult{InvocationID: row.ID, Reservation: row.Reservation, Provider: row.Provider, Model: row.Model, ReservedCost: row.Mode.Reserved}
					err = a.settleDeputyRunTx(ctx, tx, request, paid)
				} else {
					_, err = tx.Exec(ctx, `UPDATE background_usage SET reserved_cost=0 WHERE owner_id=$1 AND id=$2`, string(row.OwnerID), row.Reservation)
				}
				if err != nil {
					return err
				}
				accounting = "settled"
			}
		}
		_, err = tx.Exec(ctx, `UPDATE model_calls SET outcome=$3,error_code=$4,accounting_state=$5,finished_at=coalesce(finished_at,clock_timestamp()),actual_mode=actual_mode||jsonb_build_object('interruptedOutcome',$3::text,'interruptedAt',clock_timestamp(),'usageComplete',$6::boolean,'providerSubmitted',$7::boolean,'interruptedReason','execution_not_active'),updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2`, string(row.OwnerID), string(row.ID), outcome, code, accounting, outcome != "unknown", row.Outcome == "started")
		recorded = err == nil
		return err
	})
	return recorded, err
}
