package workspacestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/command"
)

// withUndoOf marks the pending action-log entry as the undo of another action;
// such entries never count as newer actions for the actions they sit on.
func WithUndoOf(ctx context.Context, undone string) context.Context {
	log, ok := ctx.Value(actionlog.ActionLogKey{}).(actionlog.ActionLog)
	if !ok {
		return ctx
	}
	log.UndoOf = undone
	return context.WithValue(ctx, actionlog.ActionLogKey{}, log)
}

// CommandSummary is the line of the owner's log for a command; command.Summary
// writes it with the title that storage reads.
func CommandSummary(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) string {
	var title string
	if c.Type == "completeDeadline" {
		_ = tx.QueryRow(ctx, "SELECT title FROM deadlines WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID).Scan(&title)
		return command.Summary(c, title)
	}
	title = c.Title
	if title == "" {
		title = c.Name
	}
	if id := command.Target(c); title == "" && memory.ID(id).Valid() {
		if item, err := itemstore.GetItem(ctx, tx, scope, id); err == nil {
			title = item.Title
		}
	}
	return command.Summary(c, title)
}

// A project adoption (phase 3 H7) remembers the deputy's text through a source
// of its own. Undoing the adoption deletes that source, which takes the memory
// it was the only evidence for out of the current view; the deputy result
// itself stays and can be adopted again under a new action.
func (s *Workspace) DeleteUndoneAdoptionSourcesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, actionID string) error {
	sources, err := kernel.QueryDocuments[string](ctx, tx, "SELECT to_jsonb(source_id::text) FROM project_adoption_sources WHERE owner_id=$1 AND action_id=$2", string(scope.OwnerID), actionID)
	if err != nil || len(sources) == 0 {
		return err
	}
	targets := []memory.Ref{}
	for _, id := range sources {
		targets = append(targets, memory.Ref{ID: memory.ID(id), Kind: memory.SourceKind})
	}
	return s.DeleteTx(context.WithValue(ctx, intakestore.RetainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: targets})
}

func (s *Workspace) DeleteUndoneTurnMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turnID string) error {
	var requestID string
	err := tx.QueryRow(ctx, "SELECT request_id::text FROM desk_turns WHERE owner_id=$1 AND id=$2 AND request_id IS NOT NULL", string(scope.OwnerID), turnID).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	undone, err := intakestore.DeskTurnFullyUndoneTx(ctx, tx, scope.OwnerID, requestID)
	if err != nil || !undone {
		return err
	}
	remembered, err := intakestore.DeskTurnRememberedTx(ctx, tx, scope.OwnerID, requestID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.version FROM memory_records r
	 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
	 WHERE r.owner_id=$1 AND r.kind='claim' AND c.confirmation<>'confirmed'
	 AND (c.nature IN ('plan','intention') OR NOT $3::boolean)
	 AND EXISTS(SELECT 1 FROM evidence e JOIN sources s ON (s.owner_id,s.id)=(e.owner_id,e.source_id)
	  WHERE e.owner_id=r.owner_id AND e.target_id=r.id AND s.connector='desk' AND lower(s.external_id)=lower($2))
	 AND NOT EXISTS(SELECT 1 FROM evidence e JOIN sources s ON (s.owner_id,s.id)=(e.owner_id,e.source_id)
	  WHERE e.owner_id=r.owner_id AND e.target_id=r.id AND (s.connector<>'desk' OR lower(s.external_id)<>lower($2)))
	 ORDER BY r.id`, string(scope.OwnerID), requestID, remembered)
	if err != nil {
		return err
	}
	var targets []memory.Ref
	for rows.Next() {
		ref := memory.Ref{Kind: memory.ClaimKind}
		if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// The existing deletion entry point applies artifact propagation and snapshot
	// expiration. Keep sources and allow reimport; its request limit is 100.
	for start := 0; start < len(targets); start += 100 {
		if err := s.DeleteTx(context.WithValue(ctx, intakestore.RetainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: targets[start:min(start+100, len(targets))]}); err != nil {
			return err
		}
	}
	return nil
}

// New actions have a durable insertion order; historical rows deliberately do
// not invent one. At a historical time tie, only complete unique desk receipts
// can prove order. An unresolved tie refuses safely as changed_since.
func CheckActionSuccessors(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string, changes []byte, created time.Time, order *int64, source string, turnID *string) error {
	rows, err := tx.Query(ctx, `SELECT later.id::text,later.created_at,later.action_order,later.source,later.turn_id::text
	 FROM action_log later WHERE later.owner_id=$1 AND later.id<>$2 AND later.undone_at IS NULL AND later.undo_of IS NULL
	 AND (($3::bigint IS NOT NULL AND later.action_order>$3)
	   OR ($3::bigint IS NULL AND (later.action_order IS NOT NULL OR later.created_at>=$4)))
	 AND EXISTS(SELECT 1 FROM jsonb_array_elements(later.changes) l
	   JOIN jsonb_array_elements($5::jsonb) c ON l->>'table'=c->>'table' AND l->>'id'=c->>'id')`, string(scope.OwnerID), id, order, created, changes)
	if err != nil {
		return err
	}
	type successor struct {
		id, source string
		created    time.Time
		order      *int64
		turnID     *string
	}
	var candidates []successor
	for rows.Next() {
		var next successor
		if err = rows.Scan(&next.id, &next.created, &next.order, &next.source, &next.turnID); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, next)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ambiguous := false
	var positions map[string]int
	loaded := false
	for _, next := range candidates {
		if order != nil || next.order != nil || next.created.After(created) {
			return workspace.ErrNewerAction
		}
		if source == "desk" && next.source == "desk" && turnID != nil && next.turnID != nil && *turnID == *next.turnID {
			if !loaded {
				positions, err = historicalActionPositions(ctx, tx, scope, *turnID)
				if err != nil {
					return err
				}
				loaded = true
			}
			currentPosition, currentKnown := positions[id]
			nextPosition, nextKnown := positions[next.id]
			if currentKnown && nextKnown {
				if nextPosition > currentPosition {
					return workspace.ErrNewerAction
				}
				continue
			}
		}
		ambiguous = true
	}
	if ambiguous {
		return workspace.ErrChangedSince
	}
	return nil
}

func historicalActionPositions(ctx context.Context, tx pgx.Tx, scope memory.Scope, turnID string) (map[string]int, error) {
	var response []byte
	err := tx.QueryRow(ctx, "SELECT response FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), turnID).Scan(&response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved struct {
		Turn struct {
			Receipts []struct {
				ActionID *string `json:"actionId"`
			} `json:"receipts"`
		} `json:"turn"`
	}
	if err = json.Unmarshal(response, &saved); err != nil {
		return nil, nil
	}
	positions := map[string]int{}
	for i, receipt := range saved.Turn.Receipts {
		if receipt.ActionID == nil {
			continue
		}
		if _, duplicate := positions[*receipt.ActionID]; duplicate {
			return nil, nil
		}
		positions[*receipt.ActionID] = i
	}
	rows, err := tx.Query(ctx, "SELECT id::text FROM action_log WHERE owner_id=$1 AND turn_id=$2", string(scope.OwnerID), turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	complete := true
	for rows.Next() {
		var actionID string
		if err = rows.Scan(&actionID); err != nil {
			return nil, err
		}
		if _, ok := positions[actionID]; !ok {
			complete = false
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !complete {
		return nil, nil
	}
	return positions, nil
}
