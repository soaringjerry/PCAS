package intakestore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

type SecretaryHistory struct {
	IDs       []string
	Summaries map[string]string
}

func (h *SecretaryHistory) Record(id, summary string) {
	if _, ok := h.Summaries[id]; !ok {
		h.IDs = append(h.IDs, id)
		h.Summaries[id] = summary
	}
}

func BoundedAttachmentText(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit])
	}
	return text
}

const OutdatedDeskAnswer = secretary.OutdatedAnswer

type DelegateHistoryContext struct {
	ConversationID string
	IDs            []string
}

func CurrentStudioProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string) (string, error) {
	if thing == "" {
		return "", nil
	}
	item, err := itemstore.GetItem(ctx, tx, scope, thing)
	if err != nil {
		return "", err
	}
	if item.Kind == "project" {
		return item.ID, nil
	}
	return item.ProjectID, nil
}

func RefreshSmokeChangesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, changes []actionlog.ActionChange) error {
	if actionlog.SmokeID(ctx) == "" {
		return nil
	}
	for _, c := range changes {
		hash, err := actionlog.SmokeDocumentHashTx(ctx, tx, string(scope.OwnerID), c.Table, c.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE desk_smoke_changes SET after_hash=$5 WHERE owner_id=$1 AND smoke_id=$2 AND table_name=$3 AND record_id=$4", string(scope.OwnerID), actionlog.SmokeID(ctx), c.Table, c.ID, hash); err != nil {
			return err
		}
	}
	return nil
}

// 某一轮秘书对话是否至少有一条动作记录，并且全部已撤销。
// requestID 是这一轮的请求 id，也是它的原话资料的 external_id。
// 没有这一轮，或这一轮没有任何动作记录，返回 false。
func DeskTurnFullyUndoneTx(ctx context.Context, tx pgx.Tx, ownerID memory.ID, requestID string) (bool, error) {
	var undone bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
	 SELECT 1 FROM desk_turns t
	 JOIN action_log a ON (a.owner_id,a.turn_id)=(t.owner_id,t.id)
	 WHERE t.owner_id=$1 AND lower(t.request_id::text)=lower($2) AND a.source='desk'
	 GROUP BY t.id HAVING bool_and(a.undone_at IS NOT NULL)
	)`, string(ownerID), requestID).Scan(&undone)
	return undone, err
}

func DeskTurnRememberedTx(ctx context.Context, tx pgx.Tx, ownerID memory.ID, requestID string) (bool, error) {
	var remembered bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
	 SELECT 1 FROM desk_turns WHERE owner_id=$1 AND lower(request_id::text)=lower($2)
	 AND response->'turn'->'receipts' @> '[{"op":"remember"}]'::jsonb
	)`, string(ownerID), requestID).Scan(&remembered)
	return remembered, err
}
