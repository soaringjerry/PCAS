package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func attachmentReceiptKey(scope memory.Scope, ref memory.Ref, component string) string {
	return fmt.Sprintf("attachment:%s:%s:%d:%s", scope.OwnerID, ref.ID, ref.Version, component)
}
func (s *Store) attachmentReceipt(ctx context.Context, scope memory.Scope, ref memory.Ref, component string) (*paidModelResult, error) {
	if v, ok := s.pendingPaid.Load(attachmentReceiptKey(scope, ref, component)); ok {
		return v.(*paidModelResult), nil
	}
	var out paidModelResult
	err := s.pool.QueryRow(ctx, `SELECT body FROM attachment_model_results WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND component=$4`, string(scope.OwnerID), string(ref.ID), ref.Version, component).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

// Preserve each successful page/image/transcript before any billing or derived
// source write. Failed storage reuses this exact response, including after a
// restart once the durable receipt exists.
func (s *Store) saveAttachmentReceipt(ctx context.Context, scope memory.Scope, ref memory.Ref, component string, paid *paidModelResult) error {
	key := attachmentReceiptKey(scope, ref, component)
	s.pendingPaid.Store(key, paid)
	for {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		tag, err := s.pool.Exec(persist, `INSERT INTO attachment_model_results(owner_id,source_id,source_version,component,body) SELECT $1,$2,$3,$4,$5 FROM memory_records r WHERE r.owner_id=$1 AND r.id=$2 AND r.version=$3 AND r.state='active' FOR SHARE OF r ON CONFLICT(owner_id,source_id,source_version,component) DO UPDATE SET body=attachment_model_results.body`, string(scope.OwnerID), string(ref.ID), ref.Version, component, asJSON(paid))
		cancel()
		if err == nil {
			s.pendingPaid.Delete(key)
			if tag.RowsAffected() == 0 {
				return memory.ErrNotFound
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}
func (s *Store) accountAttachmentReceipt(ctx context.Context, scope memory.Scope, ref memory.Ref, paid *paidModelResult, purpose string, job *worker.Job) error {
	usage := modelUsage{OwnerID: scope.OwnerID, ID: memory.ID(paid.Reservation), Purpose: purpose, AgentID: paid.Provider, Model: paid.Model, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost, MemoryRefs: []memory.Ref{ref}}
	if job != nil {
		usage.JobID = string(job.ID)
	}
	if err := s.recordUsage(ctx, usage); err != nil {
		return err
	}
	return s.settleModelCost(ctx, scope.OwnerID, paid.Reservation, paid.Cost)
}
