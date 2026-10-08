package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"time"
)

type actionLogKey struct{}
type actionLog struct{ id, source, turnID, summary, undoOf string }
type actionChange struct {
	Table     string          `json:"table"`
	ID        string          `json:"id"`
	Before    json.RawMessage `json:"before"`
	AfterHash *string         `json:"afterHash"`
}

func withActionLog(ctx context.Context, id, source, turnID, summary string) context.Context {
	return context.WithValue(ctx, actionLogKey{}, actionLog{id: id, source: source, turnID: turnID, summary: summary})
}

// withUndoOf marks the pending action-log entry as the undo of another action;
// such entries never count as newer actions for the actions they sit on.
func withUndoOf(ctx context.Context, undone string) context.Context {
	log, ok := ctx.Value(actionLogKey{}).(actionLog)
	if !ok {
		return ctx
	}
	log.undoOf = undone
	return context.WithValue(ctx, actionLogKey{}, log)
}
func beginActionLogTx(ctx context.Context, tx pgx.Tx) error {
	if ctx.Value(actionLogKey{}) == nil {
		return nil
	}
	_, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','[]',true)")
	return err
}
func flushActionLog(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	// Retain the audit metadata, including undone_at, after the undo window.
	// Run even when this transaction collected no new document changes.
	if _, err := tx.Exec(ctx, "UPDATE action_log SET changes='[]'::jsonb,expired_at=now() WHERE owner_id=$1 AND expired_at IS NULL AND created_at<now()-interval '30 days'", string(scope.OwnerID)); err != nil {
		return err
	}
	log, ok := ctx.Value(actionLogKey{}).(actionLog)
	if !ok {
		return nil
	}
	var changes []byte
	if err := tx.QueryRow(ctx, "SELECT current_setting('pcas.action_changes')::jsonb").Scan(&changes); err != nil {
		return err
	}
	var entries []actionChange
	if err := json.Unmarshal(changes, &entries); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','',true)"); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,turn_id,summary,changes,undo_of) VALUES($1,$2,$3,$4,$5,$6,$7)", string(scope.OwnerID), log.id, log.source, nullString(log.turnID), log.summary, changes, nullString(log.undoOf))
	if err != nil {
		return err
	}
	return recordSmokeActionTx(ctx, tx, scope, log.id, entries)
}
func undoableCommand(t string) bool {
	if t == "restoreMemory" {
		return true
	}
	return oneOf(t, "addTask", "addIdea", "addProject", "updateTask", "updateProject", "setTaskStatus", "renameThing", "setNotes", "moveThing", "deferTask", "addCheck", "toggleCheck", "removeCheck", "ideaPromote", "ideaSnooze", "ideaShelve", "ideaDrop", "ideaContinue", "addCondition", "removeCondition", "adoptRun", "discardRun", "createDoc", "updateDoc", "deleteDoc", "bulkStatus", "bulkDefer", "bulkMove")
}
func commandSummary(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) string {
	title := c.Title
	if title == "" {
		title = c.Name
	}
	id := c.ID
	if c.TaskID != "" {
		id = c.TaskID
	}
	if c.IdeaID != "" {
		id = c.IdeaID
	}
	if title == "" && memory.ID(id).Valid() {
		if item, err := getItem(ctx, tx, scope, id); err == nil {
			title = item.Title
		}
	}
	prefix := "修改："
	if oneOf(c.Type, "addTask", "addIdea", "addProject") {
		prefix = "新建："
	}
	if c.Type == "setTaskStatus" && c.Status == "done" {
		prefix = "完成："
	}
	return prefix + title
}
func (s *Store) undoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	if handled, err := undoComparisonActionTx(ctx, tx, scope, id); handled {
		return err
	}
	ctx = withActor(ctx, "user")
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	var data []byte
	var summary string
	var undone *string
	var expired bool
	var created time.Time
	var order *int64
	var source string
	var turnID *string
	err := tx.QueryRow(ctx, "SELECT changes,summary,undone_at::text,expired_at IS NOT NULL OR created_at<now()-interval '30 days',created_at,action_order,source,turn_id::text FROM action_log WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), id).Scan(&data, &summary, &undone, &expired, &created, &order, &source, &turnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	{
		var checkID string
		if e := tx.QueryRow(ctx, "SELECT smoke_id::text FROM desk_smoke_actions WHERE owner_id=$1 AND action_id=$2", string(scope.OwnerID), id).Scan(&checkID); e == nil {
			ctx = withSmoke(ctx, checkID)
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
	}
	if undone != nil {
		return workspace.ErrAlreadyUndone
	}
	// Enforce the window even before another action flushes old snapshots.
	if expired {
		return workspace.ErrExpired
	}
	var changes []actionChange
	if err = json.Unmarshal(data, &changes); err != nil {
		return err
	}
	// Recorded successors take precedence over external fingerprint changes and
	// work-start checks. The owner lock serializes all logged document writers.
	if err = checkActionSuccessors(ctx, tx, scope, id, data, created, order, source, turnID); err != nil {
		return err
	}
	// Lock all rows first. The owner lock serializes commands and undo; run locks
	// fence the worker, which does not take the owner lock when claiming work.
	for _, c := range changes {
		if c.Table == "source_attachments" {
			var ref memory.Ref
			if json.Unmarshal(c.Before, &ref) != nil || ref.Kind != memory.SourceKind || string(ref.ID) != c.ID || ref.Version < 1 {
				return memory.ErrInvalid
			}
			var active bool
			err := tx.QueryRow(ctx, "SELECT r.state='active' AND r.version=$3 AND v.blob_key IS NOT NULL FROM memory_records r JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1 AND r.id=$2 FOR UPDATE OF r", string(scope.OwnerID), c.ID, ref.Version).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
				return workspace.ErrExpired
			}
			if err != nil {
				return err
			}
			continue
		}
		if !oneOf(c.Table, "work_items", "work_documents", "agent_runs", "training_samples") {
			return memory.ErrInvalid
		}
		if c.Table == "agent_runs" && string(c.Before) == "null" {
			var status string
			e := tx.QueryRow(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&status)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && status != "queued" {
				return workspace.ErrWorkStarted
			}
		}
		if c.Table == "work_items" && string(c.Before) == "null" {
			rows, err := tx.Query(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 FOR UPDATE", string(scope.OwnerID), c.ID)
			if err != nil {
				return err
			}
			started := false
			for rows.Next() {
				var status string
				if err = rows.Scan(&status); err != nil {
					rows.Close()
					return err
				}
				if status != "queued" {
					started = true
				}
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if started {
				return workspace.ErrWorkStarted
			}
		}
		// Use the trigger's content fingerprint; retain full-document matching for
		// actions collected before migration 019, whose after snapshots are absent.
		var hash, legacyHash string
		err = tx.QueryRow(ctx, "SELECT action_document_hash(document),encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+c.Table+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&hash, &legacyHash)
		if errors.Is(err, pgx.ErrNoRows) {
			if c.AfterHash != nil {
				return workspace.ErrChangedSince
			}
		} else if err != nil {
			return err
		} else if c.AfterHash == nil || (hash != *c.AfterHash && legacyHash != *c.AfterHash) {
			return workspace.ErrChangedSince
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if c.Table == "source_attachments" {
			var ref memory.Ref
			if err := json.Unmarshal(c.Before, &ref); err != nil {
				return err
			}
			if err := s.deleteTx(context.WithValue(ctx, retainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
				return err
			}
			continue
		}
		if string(c.Before) == "null" {
			if c.Table == "work_items" {
				var referenced bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2)", string(scope.OwnerID), c.ID).Scan(&referenced); err != nil {
					return err
				}
				if referenced {
					return workspace.ErrChangedSince
				}
				// Removing queued rows releases their reserved_cost from the daily sum.
				if _, err = tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), c.ID); err != nil {
					return err
				}
			}
			_, err = tx.Exec(ctx, "DELETE FROM "+c.Table+" WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		} else {
			switch c.Table {
			case "work_items":
				var item workspace.Item
				if err = json.Unmarshal(c.Before, &item); err != nil {
					return err
				}
				current, e := getItem(ctx, tx, scope, c.ID)
				if e != nil {
					return e
				}
				if err = suppressRestoredRemindersTx(ctx, tx, scope, current, item, time.Now()); err != nil {
					return err
				}
				item.Version = current.Version + 1
				item.UpdatedAt = stamp()
				err = s.saveAction(ctx, tx, scope, item, "撤销："+summary)
			case "work_documents":
				var doc workspace.Doc
				if err = json.Unmarshal(c.Before, &doc); err == nil {
					err = restoreDocumentTx(ctx, tx, scope, doc)
				}
			case "agent_runs":
				var run workspace.Run
				if err = json.Unmarshal(c.Before, &run); err == nil {
					_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3,status=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID, c.Before, run.Status)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
	if err != nil {
		return err
	}
	if err := refreshSmokeChangesTx(ctx, tx, scope, changes); err != nil {
		return err
	}
	if source != "desk" || turnID == nil {
		return nil
	}
	return s.deleteUndoneTurnMemoriesTx(ctx, tx, scope, *turnID)
}

func (s *Store) deleteUndoneTurnMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turnID string) error {
	var requestID string
	err := tx.QueryRow(ctx, "SELECT request_id::text FROM desk_turns WHERE owner_id=$1 AND id=$2 AND request_id IS NOT NULL", string(scope.OwnerID), turnID).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	undone, err := deskTurnFullyUndoneTx(ctx, tx, scope.OwnerID, requestID)
	if err != nil || !undone {
		return err
	}
	remembered, err := deskTurnRememberedTx(ctx, tx, scope.OwnerID, requestID)
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
		if err := s.deleteTx(context.WithValue(ctx, retainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: targets[start:min(start+100, len(targets))]}); err != nil {
			return err
		}
	}
	return nil
}

// New actions have a durable insertion order; historical rows deliberately do
// not invent one. At a historical time tie, only complete unique desk receipts
// can prove order. An unresolved tie refuses safely as changed_since.
func checkActionSuccessors(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string, changes []byte, created time.Time, order *int64, source string, turnID *string) error {
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

func (s *Store) Undo(ctx context.Context, scope memory.Scope, actionID string) (workspace.State, error) {
	var out workspace.State
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.undoActionTx(ctx, tx, scope, actionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		var err error
		out, err = s.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
