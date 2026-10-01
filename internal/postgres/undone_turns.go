package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// 某一轮秘书对话是否至少有一条动作记录，并且全部已撤销。
// requestID 是这一轮的请求 id，也是它的原话资料的 external_id。
// 没有这一轮，或这一轮没有任何动作记录，返回 false。
func deskTurnFullyUndoneTx(ctx context.Context, tx pgx.Tx, ownerID memory.ID, requestID string) (bool, error) {
	var undone bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
	 SELECT 1 FROM desk_turns t
	 JOIN action_log a ON (a.owner_id,a.turn_id)=(t.owner_id,t.id)
	 WHERE t.owner_id=$1 AND lower(t.request_id)=lower($2) AND a.source='desk'
	 GROUP BY t.id HAVING bool_and(a.undone_at IS NOT NULL)
	)`, string(ownerID), requestID).Scan(&undone)
	return undone, err
}
