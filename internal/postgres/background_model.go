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
	Prompt       json.RawMessage
	Output       string
	Reservation  string
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         float64
	Refs         []memory.Ref
}

func (s *Store) paidModelResult(ctx context.Context, j worker.Job) (*paidModelResult, error) {
	r := &paidModelResult{}
	err := s.pool.QueryRow(ctx, `SELECT prompt,output,reservation_id::text,provider_id,model,input_tokens,output_tokens,cost,refs
 FROM background_model_results WHERE owner_id=$1 AND job_id=$2`, string(j.OwnerID), string(j.ID)).Scan(&r.Prompt, &r.Output, &r.Reservation, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens, &r.Cost, &r.Refs)
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
		id, err := s.reserveOrganizeCost(ctx, j, p.Reserve(instructions+string(prompt)))
		if err != nil {
			return nil, err
		}
		result, callErr := s.models.Generate(ctx, p.ID, instructions, string(prompt))
		if callErr != nil {
			if err := s.settleModelCost(ctx, j.OwnerID, id, result.Cost); err != nil {
				return nil, err
			}
			if errors.Is(callErr, memory.ErrUnavailable) {
				return nil, &worker.JobError{Code: "provider_unavailable", Until: time.Now().Add(time.Minute), NoAttempt: true}
			}
			return nil, &worker.JobError{Code: "model_call_failed", Until: time.Now().Add(retryDelay(j.Attempts)), NoAttempt: true}
		}
		saved = &paidModelResult{Prompt: prompt, Output: result.Text, Reservation: id, Provider: p.ID, Model: p.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, Cost: result.Cost, Refs: refs}
		// Stay with the same result even when storage is temporarily unavailable.
		for {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, err = s.pool.Exec(persistCtx, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(job_id) DO NOTHING`, string(j.OwnerID), string(j.ID), purpose, saved.Prompt, saved.Output, id, p.ID, p.Model, result.InputTokens, result.OutputTokens, result.Cost, asJSON(refs))
			cancel()
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return nil, err
			case <-time.After(time.Second):
			}
		}
	}
	if err := s.recordUsage(ctx, modelUsage{OwnerID: j.OwnerID, ID: memory.ID(saved.Reservation), Purpose: purpose, AgentID: saved.Provider, Model: saved.Model, InputTokens: saved.InputTokens, OutputTokens: saved.OutputTokens, Cost: saved.Cost, JobID: string(j.ID), MemoryRefs: saved.Refs}); err != nil {
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
