package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type smokeKey struct{}

func withSmoke(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, smokeKey{}, id)
}
func smokeID(ctx context.Context) string {
	id, _ := ctx.Value(smokeKey{}).(string)
	return id
}
func smokeLockTx(ctx context.Context, tx pgx.Tx, owner, id string) error {
	return secretaryTryLock(ctx, tx, "desk-smoke:"+owner+":"+id)
}

// Release the pool connection between retries, including while a model runs.
func (s *Store) smokeTransaction(ctx context.Context, work func(pgx.Tx) error) error {
	for {
		err := pgx.BeginFunc(ctx, s.pool, work)
		if !errors.Is(err, errSecretaryBusy) {
			return err
		}
		if err := secretaryPause(ctx, ctx); err != nil {
			return err
		}
	}
}

// Registration precedes admission; it also covers failed or pending turns.
// The group lock is taken before ordering/owner locks in both turn and cleanup.
func (s *Store) registerSmokeRequest(ctx context.Context, scope memory.Scope, req workspace.DeskTurnRequest) error {
	return s.smokeTransaction(ctx, func(tx pgx.Tx) error {
		owner := string(scope.OwnerID)
		if err := smokeLockTx(ctx, tx, owner, req.SmokeID); err != nil {
			return err
		}
		if err := secretaryTryLock(ctx, tx, "secretary-admission-request:"+owner+":"+req.RequestID); err != nil {
			return err
		}
		if err := secretaryTryLock(ctx, tx, "secretary-admission-conversation:"+owner+":"+req.SmokeID); err != nil {
			return err
		}
		var normal bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM desk_turns t WHERE t.owner_id=$1 AND (t.conversation_id=$2 OR t.request_id=$3)
 AND NOT EXISTS(SELECT 1 FROM desk_smoke_requests s WHERE s.owner_id=t.owner_id AND s.request_id=t.request_id)
 UNION ALL SELECT 1 FROM desk_turn_order t WHERE t.owner_id=$1 AND (t.conversation_id=$2 OR t.request_id=$3)
 AND NOT EXISTS(SELECT 1 FROM desk_smoke_requests s WHERE s.owner_id=t.owner_id AND s.request_id=t.request_id)
 UNION ALL SELECT 1 FROM sources WHERE owner_id=$1 AND connector IN ('desk','desk-incomplete','capture') AND lower(external_id)=lower($3::text)
 )`, owner, req.SmokeID, req.RequestID).Scan(&normal); err != nil {
			return err
		}
		if normal {
			return memory.ErrConflict
		}
		if _, err := tx.Exec(ctx, "INSERT INTO desk_smoke_groups(owner_id,id) VALUES($1,$2) ON CONFLICT DO NOTHING", owner, req.SmokeID); err != nil {
			return err
		}
		if err := lockSmokeTurnTx(ctx, tx, owner); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO desk_smoke_requests(owner_id,request_id,smoke_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", owner, req.RequestID, req.SmokeID); err != nil {
			return err
		}
		var group string
		if err := tx.QueryRow(ctx, "SELECT smoke_id::text FROM desk_smoke_requests WHERE owner_id=$1 AND request_id=$2", owner, req.RequestID).Scan(&group); err != nil {
			return err
		}
		if group != req.SmokeID {
			return memory.ErrConflict
		}
		return nil
	})
}

func lockSmokeTurnTx(ctx context.Context, tx pgx.Tx, owner string) error {
	id := smokeID(ctx)
	if id == "" {
		return nil
	}
	if err := smokeLockTx(ctx, tx, owner, id); err != nil {
		return err
	}
	var closed bool
	err := tx.QueryRow(ctx, "SELECT closed FROM desk_smoke_groups WHERE owner_id=$1 AND id=$2", owner, id).Scan(&closed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && closed {
		return memory.ErrConflict
	}
	return err
}

// Only server-collected changes may choose these SQL table names.
func smokeTable(table string) bool {
	return oneOf(table, "work_items", "work_documents", "agent_runs", "training_samples")
}
func smokeDocumentHashTx(ctx context.Context, tx pgx.Tx, owner, table, id string) (*string, error) {
	if !smokeTable(table) {
		return nil, memory.ErrInvalid
	}
	var hash string
	err := tx.QueryRow(ctx, "SELECT encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+table+" WHERE owner_id=$1 AND id=$2", owner, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &hash, err
}
func recordSmokeActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, actionID string, changes []actionChange) error {
	id := smokeID(ctx)
	if id == "" {
		return nil
	}
	owner := string(scope.OwnerID)
	var order int64
	if err := tx.QueryRow(ctx, "SELECT action_order FROM action_log WHERE owner_id=$1 AND id=$2", owner, actionID).Scan(&order); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO desk_smoke_actions(owner_id,action_id,smoke_id) VALUES($1,$2,$3)", owner, actionID, id); err != nil {
		return err
	}
	for _, c := range changes {
		hash, err := smokeDocumentHashTx(ctx, tx, owner, c.Table, c.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO desk_smoke_changes(owner_id,smoke_id,table_name,record_id,before_document,after_hash,action_order)
   VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(owner_id,smoke_id,table_name,record_id)
   DO UPDATE SET after_hash=excluded.after_hash,action_order=excluded.action_order`, owner, id, c.Table, c.ID, c.Before, hash, order); err != nil {
			return err
		}
	}
	return nil
}
func refreshSmokeChangesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, changes []actionChange) error {
	if smokeID(ctx) == "" {
		return nil
	}
	for _, c := range changes {
		hash, err := smokeDocumentHashTx(ctx, tx, string(scope.OwnerID), c.Table, c.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE desk_smoke_changes SET after_hash=$5 WHERE owner_id=$1 AND smoke_id=$2 AND table_name=$3 AND record_id=$4", string(scope.OwnerID), smokeID(ctx), c.Table, c.ID, hash); err != nil {
			return err
		}
	}
	return nil
}

// Cleanup serializes with turns and completion/adoption through the group and
// owner locks. It removes only this group's documents, or restores an untouched
// ordinary document; genuine later edits make the whole operation conflict.
func (s *Store) CleanupSmoke(ctx context.Context, scope memory.Scope, id string) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	id = strings.ToLower(id)
	ctx = withSmoke(ctx, id)
	if _, err := s.Snapshot(ctx, scope); err != nil {
		return err
	}
	cleanupErr := s.smokeTransaction(ctx, func(tx pgx.Tx) error {
		owner := string(scope.OwnerID)
		if err := smokeLockTx(ctx, tx, owner, id); err != nil {
			return err
		}
		var closed bool
		if err := tx.QueryRow(ctx, "SELECT closed FROM desk_smoke_groups WHERE owner_id=$1 AND id=$2", owner, id).Scan(&closed); errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrNotFound
		} else if err != nil {
			return err
		}
		if closed {
			return nil
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", owner); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT table_name,record_id::text,before_document,after_hash,action_order FROM desk_smoke_changes
   WHERE owner_id=$1 AND smoke_id=$2 ORDER BY action_order DESC,table_name,record_id`, owner, id)
		if err != nil {
			return err
		}
		type change struct {
			table, id string
			before    json.RawMessage
			after     *string
			order     int64
		}
		var changes []change
		for rows.Next() {
			var c change
			if err := rows.Scan(&c.table, &c.id, &c.before, &c.after, &c.order); err != nil {
				rows.Close()
				return err
			}
			if !smokeTable(c.table) {
				rows.Close()
				return memory.ErrInvalid
			}
			changes = append(changes, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Lock and validate everything before making any changes. Run claiming does
		// not take the owner lock, so its row lock is an additional fence.
		for _, c := range changes {
			var later bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM action_log a WHERE a.owner_id=$1 AND a.action_order>$2
    AND NOT EXISTS(SELECT 1 FROM desk_smoke_actions s WHERE s.owner_id=a.owner_id AND s.action_id=a.id AND s.smoke_id=$3)
    AND EXISTS(SELECT 1 FROM jsonb_array_elements(a.changes) v WHERE v->>'table'=$4 AND v->>'id'=$5))`, owner, c.order, id, c.table, c.id).Scan(&later); err != nil {
				return err
			}
			if later {
				return workspace.ErrChangedSince
			}
			var data []byte
			e := tx.QueryRow(ctx, "SELECT document FROM "+c.table+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", owner, c.id).Scan(&data)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if c.table == "agent_runs" && string(c.before) == "null" && e == nil {
				var run workspace.Run
				if json.Unmarshal(data, &run) != nil || run.SmokeID != id {
					return workspace.ErrChangedSince
				}
				// The group's run may have started/completed since the secretary queued
				// it. Deletion fences its late completion; usage accounting stays intact.
				continue
			}
			hash, e := smokeDocumentHashTx(ctx, tx, owner, c.table, c.id)
			if e != nil {
				return e
			}
			if (hash == nil) != (c.after == nil) || hash != nil && *hash != *c.after {
				return workspace.ErrChangedSince
			}
		}
		// Restore existing items exactly (except their monotonic version and update
		// time); this also removes check text from history/evolution/source receipts.
		for _, c := range changes {
			if string(c.before) == "null" {
				continue
			}
			switch c.table {
			case "work_items":
				var before workspace.Item
				if err := json.Unmarshal(c.before, &before); err != nil {
					return err
				}
				current, err := getItem(ctx, tx, scope, c.id)
				if err != nil {
					return err
				}
				before.Version = current.Version + 1
				before.UpdatedAt = stamp()
				if err := saveItem(ctx, tx, scope, before); err != nil {
					return err
				}
			case "work_documents":
				var before workspace.Doc
				if err := json.Unmarshal(c.before, &before); err != nil {
					return err
				}
				if err := saveDoc(ctx, tx, scope, before); err != nil {
					return err
				}
			default:
				return memory.ErrConflict // No ordinary run/sample should be rewritten by a check.
			}
		}
		// FK children first; a project can then be deleted after its check tasks.
		if _, err := tx.Exec(ctx, `DELETE FROM workspace_notices n USING desk_smoke_changes c
 WHERE n.owner_id=c.owner_id AND c.owner_id=$1 AND c.smoke_id=$2 AND c.table_name='agent_runs'
 AND c.before_document='null'::jsonb AND n.trigger_id=$3 || c.record_id::text`, owner, id, runNoticePrefix); err != nil {
			return err
		}
		// A model invocation already started is real spending. Keep its opaque
		// reservation so late settlement cannot restore a run or lose its cost.
		if _, err := tx.Exec(ctx, `INSERT INTO background_usage(owner_id,id,job_id,reserved_cost,created_at)
 SELECT r.owner_id,md5(r.owner_id::text || ':' || r.id::text || ':' || coalesce(r.document->>'createdAt',''))::uuid,NULL,r.reserved_cost,r.created_at
 FROM agent_runs r JOIN desk_smoke_changes c ON (c.owner_id,c.record_id)=(r.owner_id,r.id)
 WHERE c.owner_id=$1 AND c.smoke_id=$2 AND c.table_name='agent_runs' AND c.before_document='null'::jsonb AND r.status NOT IN ('queued','waiting')
 ON CONFLICT(id) DO NOTHING`, owner, id); err != nil {
			return err
		}
		for _, table := range []string{"training_samples", "work_documents", "agent_runs", "work_items"} {
			for _, c := range changes {
				if c.table != table || string(c.before) != "null" {
					continue
				}
				if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE owner_id=$1 AND id=$2", owner, c.id); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM action_log a USING desk_smoke_actions s
   WHERE (a.owner_id,a.id)=(s.owner_id,s.action_id) AND s.owner_id=$1 AND s.smoke_id=$2`, owner, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM desk_turns t USING desk_smoke_requests r
   WHERE (t.owner_id,t.request_id)=(r.owner_id,r.request_id) AND r.owner_id=$1 AND r.smoke_id=$2`, owner, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM desk_turn_order t USING desk_smoke_requests r
   WHERE (t.owner_id,t.request_id)=(r.owner_id,r.request_id) AND r.owner_id=$1 AND r.smoke_id=$2`, owner, id); err != nil {
			return err
		}
		for _, table := range []string{"desk_smoke_changes", "desk_smoke_actions"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE owner_id=$1 AND smoke_id=$2", owner, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE desk_smoke_groups SET closed=true WHERE owner_id=$1 AND id=$2", owner, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", owner)
		return err
	})
	var foreignKey *pgconn.PgError
	if errors.As(cleanupErr, &foreignKey) && foreignKey.Code == "23503" {
		return workspace.ErrChangedSince
	}
	return cleanupErr
}
