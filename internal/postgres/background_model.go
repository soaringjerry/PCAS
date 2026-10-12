package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/modelcall/jobcall"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type paidModelResult = modelcall.PaidResult

func (s *Store) paidModelResult(ctx context.Context, j worker.Job) (*paidModelResult, error) {
	if pending, ok := s.pendingPaid.Load(j.ID); ok {
		saved := pending.(*paidModelResult)
		current := true
		if saved.InvocationID != "" {
			if err := s.Pool.QueryRow(ctx, `SELECT recovery_state<>'replaced' FROM model_calls WHERE owner_id=$1 AND id=$2`, string(j.OwnerID), string(saved.InvocationID)).Scan(&current); err != nil {
				return nil, err
			}
		}
		if current {
			return saved, nil
		}
		s.pendingPaid.CompareAndDelete(j.ID, saved)
	}
	r := &paidModelResult{}
	err := s.Pool.QueryRow(ctx, `SELECT b.prompt,b.output,b.reservation_id::text,b.provider_id,b.model,b.input_tokens,b.output_tokens,b.cost,b.refs,b.input_estimated,b.output_estimated,b.cost_estimated,b.duration_ms,coalesce(c.id::text,''),coalesce(c.error_code,''),coalesce((c.actual_mode->>'reservationEstimate')::double precision,0),coalesce(c.result_receipt->'searches','[]'::jsonb)
 FROM background_model_results b LEFT JOIN model_calls c ON c.owner_id=b.owner_id AND c.reservation_id=b.reservation_id WHERE b.owner_id=$1 AND b.job_id=$2 AND b.purpose<>'deputy_input'`, string(j.OwnerID), string(j.ID)).Scan(&r.Prompt, &r.Output, &r.Reservation, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens, &r.Cost, &r.Refs, &r.InputEstimated, &r.OutputEstimated, &r.CostEstimated, &r.DurationMS, &r.InvocationID, &r.CallErrorCode, &r.ReservedCost, &r.Searches)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// JobCallStore gives the background call generator the paid results and the
// reservations of a job.
func (s *Store) JobCallStore() jobcall.Store { return jobCallStore{s} }

type jobCallStore struct{ store *Store }

func (a jobCallStore) SavedResult(ctx context.Context, j worker.Job) (*modelcall.PaidResult, error) {
	return a.store.paidModelResult(ctx, j)
}

func (a jobCallStore) UnknownRecovery(ctx context.Context, j worker.Job) error {
	return backgroundCalls{store: a.store}.UnknownRecovery(ctx, j)
}

func (a jobCallStore) ReleaseUnavailable(ctx context.Context, j worker.Job, reservation string) error {
	return a.store.releaseUnavailableReservation(ctx, j, reservation)
}

func (a jobCallStore) HourlyLimited(stage string) bool {
	return backgroundHourlyBudgets[backgroundStage(stage)] != 0
}

func (a jobCallStore) RetryDelay(attempts int) time.Duration { return kernel.RetryDelay(attempts) }
