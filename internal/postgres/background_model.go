package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type paidModelResult struct {
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

func (s *Store) paidModelResult(ctx context.Context, j worker.Job) (*paidModelResult, error) {
	if pending, ok := s.pendingPaid.Load(j.ID); ok {
		return pending.(*paidModelResult), nil
	}
	r := &paidModelResult{}
	err := s.pool.QueryRow(ctx, `SELECT prompt,output,reservation_id::text,provider_id,model,input_tokens,output_tokens,cost,refs,input_estimated,output_estimated,cost_estimated,duration_ms
 FROM background_model_results WHERE owner_id=$1 AND job_id=$2`, string(j.OwnerID), string(j.ID)).Scan(&r.Prompt, &r.Output, &r.Reservation, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens, &r.Cost, &r.Refs, &r.InputEstimated, &r.OutputEstimated, &r.CostEstimated, &r.DurationMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// Retry the paid result, including its original input, before any business write.
// A recovered job reuses this snapshot rather than generating against new input.
func (s *Store) generatePaid(ctx context.Context, j worker.Job, purpose, instructions string, prompt json.RawMessage, refs []memory.Ref) (*paidModelResult, error) {
	saved, err := s.paidModelResult(ctx, j)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		p, ok := s.models.Get(s.models.ExtractionID())
		if !ok {
			return nil, &worker.JobError{Code: "provider_not_configured"}
		}
		if !s.models.Available(p.ID) {
			return nil, &worker.JobError{Code: "provider_unavailable", Until: time.Now().Add(time.Minute), NoAttempt: true}
		}
		modelPrompt := string(prompt)
		var wrapped struct {
			RawPrompt string `json:"rawPrompt"`
		}
		if json.Unmarshal(prompt, &wrapped) == nil && wrapped.RawPrompt != "" {
			modelPrompt = wrapped.RawPrompt
		}
		id, err := s.reserveModelCostID(ctx, j.OwnerID, p.Reserve(instructions+modelPrompt), &j)
		if err != nil {
			return nil, err
		}
		result, callErr := s.models.Generate(ctx, p.ID, instructions, modelPrompt)
		if callErr != nil {
			// Account even failed/partial calls; no call was made on unavailable providers.
			if !errors.Is(callErr, memory.ErrUnavailable) {
				if err := s.recordUsage(ctx, modelUsage{OwnerID: j.OwnerID, ID: memory.ID(id), Purpose: purpose, AgentID: p.ID, Model: p.Model, DurationMS: result.DurationMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, InputEstimated: result.InputEstimated, OutputEstimated: result.OutputEstimated, CostEstimated: result.CostEstimated, Cost: result.Cost, JobID: string(j.ID), MemoryRefs: refs}); err != nil {
					return nil, err
				}
			}
			if err := s.settleModelCost(ctx, j.OwnerID, id, result.Cost); err != nil {
				return nil, err
			}
			if backgroundHourlyBudgets[backgroundStage(j.Stage)] == 0 {
				if errors.Is(callErr, memory.ErrUnavailable) {
					if err := s.releaseUnavailableReservation(ctx, j, id); err != nil {
						return nil, err
					}
					return nil, &worker.JobError{Code: "provider_unavailable", Retry: true}
				}
				return nil, &worker.JobError{Code: "model_call_failed", Retry: p.Reserve(instructions+modelPrompt) == 0}
			}
			return nil, &worker.JobError{Code: "model_call_failed", Until: time.Now().Add(retryDelay(j.Attempts))}
		}
		saved = &paidModelResult{Prompt: prompt, Output: result.Text, Reservation: id, Provider: p.ID, Model: p.Model, DurationMS: result.DurationMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, InputEstimated: result.InputEstimated, OutputEstimated: result.OutputEstimated, CostEstimated: result.CostEstimated, Cost: result.Cost, Refs: refs}
		// A canceled lease can retry persistence in this process without losing
		// the response. Once stored, restart recovery uses the durable snapshot.
		s.pendingPaid.Store(j.ID, saved)
	}
	for {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, err = s.pool.Exec(persistCtx, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs,input_estimated,output_estimated,cost_estimated,duration_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(job_id) DO NOTHING`, string(j.OwnerID), string(j.ID), purpose, saved.Prompt, saved.Output, saved.Reservation, saved.Provider, saved.Model, saved.InputTokens, saved.OutputTokens, saved.Cost, asJSON(saved.Refs), saved.InputEstimated, saved.OutputEstimated, saved.CostEstimated, saved.DurationMS)
		cancel()
		if err == nil {
			s.pendingPaid.Delete(j.ID)
			break
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(time.Second):
		}
	}

	if err := s.recordUsage(ctx, modelUsage{OwnerID: j.OwnerID, ID: memory.ID(saved.Reservation), Purpose: purpose, AgentID: saved.Provider, Model: saved.Model, DurationMS: saved.DurationMS, InputTokens: saved.InputTokens, OutputTokens: saved.OutputTokens, InputEstimated: saved.InputEstimated, OutputEstimated: saved.OutputEstimated, CostEstimated: saved.CostEstimated, Cost: saved.Cost, JobID: string(j.ID), MemoryRefs: saved.Refs}); err != nil {
		return nil, err
	}
	if err := s.settleModelCost(ctx, j.OwnerID, saved.Reservation, saved.Cost); err != nil {
		return nil, err
	}
	return saved, nil
}

func discardPaidResultTx(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	_, err := tx.Exec(ctx, "DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=$2", string(j.OwnerID), string(j.ID))
	return err
}
