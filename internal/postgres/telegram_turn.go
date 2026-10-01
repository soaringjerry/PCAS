package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// DeskTurnByRequest reads the existing desk result without resubmitting input.
// Telegram uses this before downloading/transcribing a redelivered voice.
// It performs the same owner, dependency and live undo checks as DeskTurns;
// erased responses remain erased and no additional response cache is written.
func (s *Store) DeskTurnByRequest(ctx context.Context, scope memory.Scope, requestID string) (workspace.DeskTurnResponse, error) {
	var out workspace.DeskTurnResponse
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(requestID).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var response []byte
		var dependencies []byte
		var agent string
		var erased bool
		err := tx.QueryRow(ctx, "SELECT response,dependencies,agent_id,question='' AND answer='' FROM desk_turns WHERE owner_id=$1 AND request_id=$2 AND response IS NOT NULL", string(scope.OwnerID), requestID).Scan(&response, &dependencies, &agent, &erased)
		if errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrNotFound
		}
		if err != nil {
			return err
		}
		var saved storedSecretaryResponse
		if err = json.Unmarshal(response, &saved); err != nil {
			return err
		}
		var refs []memory.Ref
		if err = json.Unmarshal(dependencies, &refs); err != nil {
			return err
		}
		out.ConversationID, out.Turn = saved.ConversationID, saved.Turn
		if erased || len(refs) > 0 && verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: agent, ContextVersions: refs}) != nil {
			out.Turn.Reply = "（这条回答依据的记忆已变更）"
			out.Turn.Cards = []workspace.DeskCard{}
		}
		if err = refreshDeskReceiptUndoTx(ctx, tx, scope, []workspace.SecretaryTurn{out.Turn}); err != nil {
			return err
		}
		out.State, err = s.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
