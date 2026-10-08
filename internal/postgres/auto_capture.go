package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Twenty automatic tasks per original/whole conversation prevents an imported
// list from flooding the hall. Every excess item remains a counted candidate.
const automaticTaskLimit = 20
const automaticCaptureConfidence = 0.98

func captureReasons(source memory.SourceResult, item extractedItem, imported bool) ([]string, string) {
	reasons, descriptions := []string{}, []string{}
	add := func(code, text string) { reasons = append(reasons, code); descriptions = append(descriptions, text) }
	if imported || source.Context != nil && source.Context.Branch == "historical" {
		add("historical", "历史导入，不作为当前事项")
	}
	if item.Qualification == "ai_suggestion" || source.Context != nil && source.Context.Role != "user" {
		add("ai_suggestion", "AI 或工具的建议，不是本人表达")
	}
	if item.Acquisition == "reported" || item.Qualification == "quoted" {
		add("reported", "转述，留作候选")
	}
	if item.Qualification == "tentative" || !item.Explicit && item.Acquisition == "direct" {
		add("qualified", "带保留，留作候选")
	}
	if item.Acquisition == "inferred" || item.Confidence < automaticCaptureConfidence || !oneOf(item.Qualification, "", "asserted", "corrected") || !item.Explicit && item.Acquisition != "direct" {
		add("uncertain", "拿不准，留作候选")
	}
	return reasons, strings.Join(descriptions, "；")
}

// Candidate acceptance is deliberately outside action tracking: its retained
// accepted row is the replay fence even after undo deletes the work item.
func (s *Store) captureActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, source memory.SourceResult, item extractedItem, imported bool, project string) error {
	if !oneOf(item.Kind, "task", "idea") {
		return memory.ErrInvalid
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM capture_candidates WHERE owner_id=$1 AND source_id=$2
 AND document->>'kind'=$3 AND (document->'source'->>'excerpt'=$4 OR document->>'text'=$5))`, string(scope.OwnerID), string(source.Source.Ref.ID), item.Kind, item.Quote, item.Text).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return nil
	}
	reasons, reason := captureReasons(source, item, imported)
	v := workspace.Candidate{ID: string(memory.NewID()), Kind: item.Kind, Text: item.Text, ProjectID: project, Confidence: item.Confidence, Source: workspace.SourceRef{SourceID: string(source.Source.Ref.ID), Version: source.Source.Ref.Version, Label: source.Source.Title, Excerpt: item.Quote, At: stamp()}, State: "pending", CreatedAt: stamp(), Reasons: reasons, Reason: reason}
	if len(reasons) == 0 && item.Kind == "task" {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM capture_candidates c WHERE c.owner_id=$1 AND c.document->>'kind'='task' AND c.state='accepted'
 AND (c.source_id=$2 OR $3<>'' AND EXISTS(SELECT 1 FROM source_contexts sc WHERE sc.owner_id=c.owner_id AND sc.source_id=c.source_id AND sc.conversation_key=$3))`, string(scope.OwnerID), string(source.Source.Ref.ID), conversationKey(source)).Scan(&count)
		if err != nil {
			return err
		}
		if count >= automaticTaskLimit {
			v.Reasons = append(v.Reasons, "overflow")
			v.Reason = "超过本段对话或资料自动创建上限，保留为候选"
			if err := stageEventTx(ctx, tx, scope.OwnerID, "source.extract", "overflow", "automatic_task_limit", 1); err != nil {
				return err
			}
		}
	}
	if len(v.Reasons) == 0 && len(v.Text) > 2000 {
		v.Reasons = append(v.Reasons, "overflow")
		v.Reason = "事项标题超过既有2000字节上限，完整内容保留为候选"
		if err := stageEventTx(ctx, tx, scope.OwnerID, "source.extract", "overflow", "automatic_title_bytes", len(v.Text)-2000); err != nil {
			return err
		}
	}
	if len(v.Reasons) == 0 {
		item := newItem(v.Kind, v.Text)
		item.History[0].By = "ai"
		item.Evolution[0].By = "ai"
		item.ProjectID = project
		item.Sources = append(item.Sources, v.Source)
		item.Creation = &workspace.ItemCreation{By: "background_extraction", Source: &v.Source}
		actionID := string(memory.NewID())
		label := map[string]string{"task": "待办", "idea": "想法"}[v.Kind]
		actionCtx := withActionLog(withActor(ctx, "ai"), actionID, "background_extraction", "", "建了"+label+"「"+v.Text+"」")
		if err := beginActionLogTx(actionCtx, tx); err != nil {
			return err
		}
		if err := saveItem(actionCtx, tx, scope, item); err != nil {
			return err
		}
		if err := flushActionLog(actionCtx, tx, scope); err != nil {
			return err
		}
		v.State = "accepted"
		v.ResolvedInto = item.ID
	}
	return saveCandidate(ctx, tx, scope, v)
}
func conversationKey(source memory.SourceResult) string {
	if source.Context != nil {
		return source.Context.Conversation
	}
	return ""
}
