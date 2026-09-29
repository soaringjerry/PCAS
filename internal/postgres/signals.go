package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type conditionSignal struct {
	IdeaID      string  `json:"idea_id"`
	ConditionID string  `json:"condition_id"`
	Quote       string  `json:"quote"`
	Explanation string  `json:"explanation"`
	Confidence  float64 `json:"confidence"`
}

func (s *Store) pendingConditions(ctx context.Context, scope memory.Scope) ([]map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.document FROM work_items i JOIN workspace_owners w USING(owner_id) WHERE i.owner_id=$1 AND i.kind='idea' AND i.status='shelved' AND (i.document->>'remindersOn')::boolean AND (w.settings->>'wakeIdeas')::boolean ORDER BY i.updated_at DESC LIMIT 20`, string(scope.OwnerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]string{}
	for rows.Next() {
		var item workspace.Item
		if err := rows.Scan(&item); err != nil {
			return nil, err
		}
		for _, c := range item.Conditions {
			if c.Kind == "event" && !c.Met && len(result) < 30 {
				result = append(result, map[string]string{"idea_id": item.ID, "condition_id": c.ID, "idea": item.Title, "condition": c.Description})
			}
		}
	}
	return result, rows.Err()
}
func (s *Store) applySignalsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, source memory.Source, signals []conditionSignal) error {
	if len(signals) > 30 {
		return memory.ErrInvalid
	}
	for _, signal := range signals {
		if signal.Confidence < 0.98 || signal.Confidence > 1 || signal.Quote == "" || !strings.Contains(source.Text, signal.Quote) || requireText(signal.Explanation) != nil || len(signal.Explanation) > 2000 || !memory.ID(signal.IdeaID).Valid() {
			continue
		}
		item, err := getItem(ctx, tx, scope, signal.IdeaID)
		if errors.Is(err, memory.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if item.Kind != "idea" || item.Status != "shelved" || !item.RemindersOn || item.Wake != nil {
			continue
		}
		for i := range item.Conditions {
			c := &item.Conditions[i]
			if c.ID != signal.ConditionID || c.Kind != "event" {
				continue
			}
			reason := "发现相关线索（待核验）：" + signal.Explanation
			tag, err := tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason,source_id,source_version) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, string(scope.OwnerID), item.ID, c.ID, source.RecordedAt, reason, string(source.ID), source.Version)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			c.MetBy = &workspace.SourceRef{SourceID: string(source.ID), Version: source.Version, Label: source.Title, Excerpt: signal.Quote, At: stamp()}
			// A model finding relevant evidence does not confirm the condition.
			item.Status = "awakened"
			item.Wake = &workspace.Wake{At: stamp(), Reason: reason, ConditionID: c.ID}
			item.Evolution = append(item.Evolution, workspace.Revision{At: stamp(), By: "ai", Summary: reason, SourceID: string(source.ID)})
			item.Version++
			item.UpdatedAt = stamp()
			if err := saveItem(ctx, tx, scope, item); err != nil {
				return err
			}
		}
	}
	return nil
}
