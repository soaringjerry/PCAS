package deputystore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usagelog"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
)

func validateReviseTargetTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM revise_run_targets t JOIN work_documents d ON(d.owner_id,d.id)=(t.owner_id,t.document_id) WHERE t.owner_id=$1 AND t.run_id=$2 AND (d.document->>'version')::integer=t.target_version)`, string(scope.OwnerID), run.ID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return memory.ErrConflict
	}
	return nil
}
func (s *Deputy) adoptReviseDocumentTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run, text string) error {
	if err := validateReviseTargetTx(ctx, tx, scope, run); err != nil {
		return err
	}
	base, err := workspacestore.DocumentVersionTx(ctx, tx, scope, run.DocumentID, run.BaseVersion)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" || strings.TrimSpace(text) == strings.TrimSpace(base.Body) {
		return memory.ErrInvalid
	}
	doc, err := kernel.QueryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.DocumentID)
	if err != nil {
		return err
	}
	if doc.ThingID != run.ThingID {
		return memory.ErrConflict
	}
	doc.Body = text
	doc.By = "deputy"
	doc.RunID = run.ID
	doc.BasedOn = &run.BaseVersion
	doc.UpdatedAt = kernel.Stamp()
	return workspacestore.SaveDoc(ctx, tx, scope, doc)
}

func (s *Deputy) finishPaidRevise(ctx context.Context, scope memory.Scope, current workspace.Run, token string, paid *callstore.PaidModelResult) (resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = pgx.BeginFunc(retryCtx, s.Pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(retryCtx, `UPDATE agent_runs SET lease_until=clock_timestamp(),document=document||jsonb_build_object('error',$4::text) WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND status='running'`, string(scope.OwnerID), current.ID, token, deputy.RevisionWriteRetry)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			return kernel.StageEvent(retryCtx, tx, scope.OwnerID, "deputy", "failure", "revise_result_write_"+budgetstore.BackgroundFailureReason(resultErr), 1)
		})
	}()
	var snapshot deputy.Snapshot
	if err := json.Unmarshal(paid.Prompt, &snapshot); err != nil {
		return err
	}
	run := snapshot.Run
	if paid.InvocationID == "" {
		if err := s.RecordUsage(ctx, usagelog.ModelUsage{OwnerID: scope.OwnerID, ID: memory.ID(paid.Reservation), Purpose: "deputy", AgentID: paid.Provider, Model: paid.Model, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost, RunID: run.ID, MemoryRefs: paid.Refs, Tier: run.MemoryTier}); err != nil {
			return err
		}
		if err := s.SettleRunCost(ctx, scope.OwnerID, run, paid.Cost); err != nil {
			return err
		}
	}
	return kernel.BackgroundResultTx(ctx, s.Pool, scope.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND lease_until>clock_timestamp() AND status='running' AND document->>'createdAt'=$4)`, string(scope.OwnerID), current.ID, token, run.CreatedAt).Scan(&active); err != nil {
			return err
		}
		if !active {
			return worker.ErrLeaseLost
		}
		ending, invalid := deputy.FinishRevision(ctx, revisionUnit{finishUnit{tx: tx, scope: scope}}, run, paid.Output, paid.Cost, time.Now())
		run = ending.Run
		if invalid {
			if err := kernel.StageEvent(ctx, tx, scope.OwnerID, "deputy", "failure", "revise_invalid_output", 1); err != nil {
				return err
			}
		}
		for _, ref := range run.ContextVersions {
			if _, err := tx.Exec(ctx, `INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) SELECT $1,$2,record_id,version FROM record_versions WHERE owner_id=$1 AND record_id=$3 AND version=$4 ON CONFLICT DO NOTHING`, string(scope.OwnerID), run.ID, string(ref.ID), ref.Version); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agent_runs SET status=$4,document=$5,reserved_cost=$6,lease_token=NULL,lease_until=NULL WHERE owner_id=$1 AND id=$2 AND lease_token=$3`, string(scope.OwnerID), run.ID, token, run.Status, kernel.JSON(run), run.Cost); err != nil {
			return err
		}
		if run.Status == "done" && !run.StaleContext {
			if err := s.autoAdoptResultTx(ctx, tx, scope, &run); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,clock_timestamp(),$4) ON CONFLICT DO NOTHING`, string(scope.OwnerID), run.ThingID, kernel.RunNoticePrefix+run.ID, ending.Notice); err != nil {
			return err
		}
		if paid.InvocationID != "" {
			return (callstore.InteractiveCalls{Store: s.Calls}).RecordDeputyApplicationTx(ctx, tx, scope.OwnerID, run, paid.InvocationID, ending.Answer.Outcome, ending.Answer.Reason)
		}
		return kernel.DiscardPaidResult(ctx, tx, worker.Job{OwnerID: scope.OwnerID, ID: memory.ID(run.ID)})
	})
}

// D5 permits undoing the inverse of a revise adoption. Keep this extension
// confined to actions that changed a revise run; ordinary undo stays as before.
func ReviseUndoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (bool, error) {
	if !memory.ID(id).Valid() {
		return false, nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM action_log a CROSS JOIN LATERAL jsonb_array_elements(a.changes) c JOIN agent_runs r ON r.owner_id=a.owner_id AND r.id::text=c->>'id' WHERE a.owner_id=$1 AND a.id=$2 AND c->>'table'='agent_runs' AND r.document->>'kind'='revise')`, string(scope.OwnerID), id).Scan(&ok)
	return ok, err
}

// A pasted revision has no model-side complete flag, so the program checks the
// obvious structural defects only (contract D4): empty, identical to the base or
// the current version, or shorter than a quarter of the base — a revision that
// keeps every unrequested paragraph cannot shrink that far. Wording is left to
// the model/user.
func manualReviseReasonTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run, text string) (string, error) {
	base, err := workspacestore.DocumentVersionTx(ctx, tx, scope, run.DocumentID, run.BaseVersion)
	if err != nil {
		return deputy.RevisionRejection(text, nil, ""), nil
	}
	doc, err := kernel.QueryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.DocumentID)
	if err != nil {
		return "", err
	}
	return deputy.RevisionRejection(text, &base.Body, doc.Body), nil
}

// revisionUnit checks a paid revision in the transaction of its last write.
type revisionUnit struct{ finishUnit }

func (u revisionUnit) Base(ctx context.Context, run workspace.Run) (string, bool) {
	base, err := workspacestore.DocumentVersionTx(ctx, u.tx, u.scope, run.DocumentID, run.BaseVersion)
	return base.Body, err == nil
}

func (u revisionUnit) TargetCurrent(ctx context.Context, run workspace.Run) bool {
	return validateReviseTargetTx(ctx, u.tx, u.scope, run) == nil
}
