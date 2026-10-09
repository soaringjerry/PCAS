package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"strings"
	"time"
)

type reviseSnapshot struct {
	Run         workspace.Run `json:"run"`
	RawPrompt   string        `json:"rawPrompt,omitempty"`
	Coverage    *useCoverage  `json:"coverage,omitempty"`
	MemoryReady bool          `json:"memoryReady,omitempty"`
}

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
func (s *Store) adoptReviseDocumentTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run, text string) error {
	if err := validateReviseTargetTx(ctx, tx, scope, run); err != nil {
		return err
	}
	base, err := documentVersionTx(ctx, tx, scope, run.DocumentID, run.BaseVersion)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" || strings.TrimSpace(text) == strings.TrimSpace(base.Body) {
		return memory.ErrInvalid
	}
	doc, err := queryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.DocumentID)
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
	doc.UpdatedAt = stamp()
	return saveDoc(ctx, tx, scope, doc)
}

func (s *Store) finishPaidRevise(ctx context.Context, scope memory.Scope, current workspace.Run, token string, paid *paidModelResult) (resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = pgx.BeginFunc(retryCtx, s.pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(retryCtx, `UPDATE agent_runs SET lease_until=clock_timestamp(),document=document||jsonb_build_object('error','模型结果已保存，文档写入失败，正在重试写入') WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND status='running'`, string(scope.OwnerID), current.ID, token)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			return stageEventTx(retryCtx, tx, scope.OwnerID, "deputy", "failure", "revise_result_write_"+backgroundFailureReason(resultErr), 1)
		})
	}()
	var snapshot reviseSnapshot
	if err := json.Unmarshal(paid.Prompt, &snapshot); err != nil {
		return err
	}
	run := snapshot.Run
	var result struct {
		Body     string `json:"body"`
		Complete *bool  `json:"complete"`
	}
	reason := ""
	if json.Unmarshal([]byte(paid.Output), &result) != nil {
		reason = "改写输出格式不合格"
	} else if strings.TrimSpace(result.Body) == "" {
		reason = "改写正文为空"
	} else if result.Complete == nil || !*result.Complete {
		reason = "副手未输出完整正文（截断或未完成）"
	}
	if paid.InvocationID == "" {
		if err := s.recordUsage(ctx, modelUsage{OwnerID: scope.OwnerID, ID: memory.ID(paid.Reservation), Purpose: "deputy", AgentID: paid.Provider, Model: paid.Model, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost, RunID: run.ID, MemoryRefs: paid.Refs, Tier: run.MemoryTier}); err != nil {
			return err
		}
		if err := s.settleRunCost(ctx, scope.OwnerID, run, paid.Cost); err != nil {
			return err
		}
	}
	return backgroundResultTx(ctx, s.pool, scope.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
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
		base, err := documentVersionTx(ctx, tx, scope, run.DocumentID, run.BaseVersion)
		if err != nil {
			reason = "基准文档已删除或版本不存在"
		} else if strings.TrimSpace(result.Body) == strings.TrimSpace(base.Body) {
			reason = "改写正文与基准完全相同"
		}
		run.Status = "done"
		run.Output = result.Body
		run.FinishedAt = stamp()
		run.Cost = paid.Cost
		if reason != "" {
			run.Status = "failed"
			run.Error = reason
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "failure", "revise_invalid_output", 1); err != nil {
				return err
			}
		}
		if reason == "" {
			if err := validateReviseTargetTx(ctx, tx, scope, run); err != nil {
				run.StaleContext = true
				run.Error = "目标文档在工作期间已改变；结果保留，未覆盖当前文档"
			} else if checkUseRunPromptTx(ctx, tx, scope, run) != nil {
				run.StaleContext = true
				run.Error = "事项在工作期间已改变；结果保留，未采纳"
			}
		}
		if verifyRunAccessTx(ctx, tx, scope, run) != nil {
			run.StaleContext = true
			run.Status = "failed"
			run.Output = ""
			run.Error = "工作资料已删除或访问状态已改变"
		}
		for _, ref := range run.ContextVersions {
			if _, err := tx.Exec(ctx, `INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) SELECT $1,$2,record_id,version FROM record_versions WHERE owner_id=$1 AND record_id=$3 AND version=$4 ON CONFLICT DO NOTHING`, string(scope.OwnerID), run.ID, string(ref.ID), ref.Version); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agent_runs SET status=$4,document=$5,reserved_cost=$6,lease_token=NULL,lease_until=NULL WHERE owner_id=$1 AND id=$2 AND lease_token=$3`, string(scope.OwnerID), run.ID, token, run.Status, asJSON(run), run.Cost); err != nil {
			return err
		}
		if run.Status == "done" && !run.StaleContext {
			if err := s.autoAdoptResultTx(ctx, tx, scope, &run); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,clock_timestamp(),$4) ON CONFLICT DO NOTHING`, string(scope.OwnerID), run.ThingID, runNoticePrefix+run.ID, runNoticeReason(run)); err != nil {
			return err
		}
		if paid.InvocationID != "" {
			outcome, why := "applied", ""
			if run.Status != "done" || run.StaleContext {
				outcome, why = "not_applicable", "revision_not_adopted"
			}
			return (interactiveCalls{store: s}).recordDeputyApplicationTx(ctx, tx, scope.OwnerID, run, paid.InvocationID, outcome, why)
		}
		return discardPaidResultTx(ctx, tx, worker.Job{OwnerID: scope.OwnerID, ID: memory.ID(run.ID)})
	})
}

// D5 permits undoing the inverse of a revise adoption. Keep this extension
// confined to actions that changed a revise run; ordinary undo stays as before.
func reviseUndoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (bool, error) {
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
	text = strings.TrimSpace(text)
	if text == "" {
		return "改写正文为空", nil
	}
	base, err := documentVersionTx(ctx, tx, scope, run.DocumentID, run.BaseVersion)
	if err != nil {
		return "基准文档已删除或版本不存在", nil
	}
	doc, err := queryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.DocumentID)
	if err != nil {
		return "", err
	}
	baseBody := strings.TrimSpace(base.Body)
	if text == baseBody {
		return "改写正文与基准完全相同", nil
	}
	if text == strings.TrimSpace(doc.Body) {
		return "改写正文与当前版本完全相同", nil
	}
	if len([]rune(baseBody)) >= 40 && len([]rune(text))*4 < len([]rune(baseBody)) {
		return "改写正文明显短于基准，疑似截断；未存为新版本", nil
	}
	return "", nil
}
