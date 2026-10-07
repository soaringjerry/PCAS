package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type retirementSnapshot struct {
	Retired  string     `json:"retired"`
	By       *string    `json:"by"`
	At       *time.Time `json:"at"`
	Compared int        `json:"compared"`
	Version  int        `json:"version"`
}

func retirementSnapshotTx(ctx context.Context, tx pgx.Tx, owner memory.ID, id string) (retirementSnapshot, error) {
	var out retirementSnapshot
	err := tx.QueryRow(ctx, `SELECT cl.retired,cl.retired_by::text,cl.retired_at,cl.compared,r.version
 FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 WHERE cl.owner_id=$1 AND cl.id=$2 AND r.state='active' FOR UPDATE OF cl,r`, string(owner), id).Scan(&out.Retired, &out.By, &out.At, &out.Compared, &out.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = memory.ErrNotFound
	}
	return out, err
}

// Done restoration markers belong to the comparison namespace, so queue
// cleanup for unrelated processing cannot discard a comparison exemption.
// Recognize the original spelling too for restores made before this fix.
func restoredMemorySQL(rule string) string {
	return `EXISTS(SELECT 1 FROM background_markers restored WHERE restored.owner_id=cl.owner_id AND restored.record_id=cl.id AND (restored.stage LIKE 'memory.compare:'||` + rule + `::int::text||':restored:%' OR restored.stage LIKE 'memory.compare_restored:'||` + rule + `::int::text||':%'))`
}

func retirementFingerprint(state retirementSnapshot) string {
	return fmt.Sprintf("%x", sha256.Sum256(asJSON(state)))
}

func restoreMemoryTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	before, err := retirementSnapshotTx(ctx, tx, scope.OwnerID, id)
	if err != nil {
		return err
	}
	if before.Retired == "" {
		return memory.ErrConflict
	}
	var current bool
	if err := tx.QueryRow(ctx, "SELECT claim_source_is_current($1,$2,$3,now())", string(scope.OwnerID), id, before.Version).Scan(&current); err != nil {
		return err
	}
	if !current {
		return memory.ErrConflict
	}
	if _, err := tx.Exec(ctx, "UPDATE claims SET retired='',retired_by=NULL,retired_at=NULL,compared=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, CompareVersion); err != nil {
		return err
	}
	after, err := retirementSnapshotTx(ctx, tx, scope.OwnerID, id)
	if err != nil {
		return err
	}
	log, ok := ctx.Value(actionLogKey{}).(actionLog)
	if !ok {
		return memory.ErrInvalid
	}
	// The action ledger expires snapshots after 30 days. Keep the rule-version
	// protection independently, so expiration never silently retires a restored
	// memory again. This done marker carries IDs only and is not worker work.
	if _, err := tx.Exec(ctx, `INSERT INTO background_markers(owner_id,record_id,record_version,stage)
 VALUES($1,$2,$3,$4)`, string(scope.OwnerID), id, before.Version, fmt.Sprintf("%s:%d:restored:%s", CompareStage, CompareVersion, log.id)); err != nil {
		return err
	}
	changes := asJSON([]map[string]any{{"table": "claim_retirement", "id": id, "before": before, "afterHash": retirementFingerprint(after), "rule": CompareVersion}})
	if _, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,summary,changes) VALUES($1,$2,'command','恢复退出的记忆',$3)", string(scope.OwnerID), log.id, changes); err != nil {
		return err
	}
	return nil
}

func (s *Store) undoEntityCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	var actionID string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM action_log a WHERE a.owner_id=$1 AND a.undone_at IS NULL
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(a.changes) c WHERE c->>'table'='entity_merge' AND c->>'id'=$2)
 ORDER BY a.action_order DESC NULLS LAST,a.created_at DESC LIMIT 1`, string(scope.OwnerID), id).Scan(&actionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.undoActionTx(ctx, tx, scope, actionID)
}

// The existing action ledger owns request replay, expiry and permission checks.
// These two derived changes do not have document triggers, so their snapshots
// are collected explicitly and restored here before the generic document path.
func undoComparisonActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (bool, error) {
	if !memory.ID(id).Valid() {
		return false, nil
	}
	var data []byte
	var undone, expired bool
	var created time.Time
	var order *int64
	var source string
	var turnID *string
	err := tx.QueryRow(ctx, `SELECT changes,undone_at IS NOT NULL,expired_at IS NOT NULL OR created_at<now()-interval '30 days',created_at,action_order,source,turn_id::text
 FROM action_log WHERE owner_id=$1 AND id=$2 FOR UPDATE`, string(scope.OwnerID), id).Scan(&data, &undone, &expired, &created, &order, &source, &turnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var entries []struct {
		Table     string          `json:"table"`
		ID        string          `json:"id"`
		Before    json.RawMessage `json:"before"`
		AfterHash string          `json:"afterHash"`
	}
	if json.Unmarshal(data, &entries) != nil || len(entries) != 1 || !oneOf(entries[0].Table, "claim_retirement", "entity_merge") {
		return false, nil
	}
	if undone {
		return true, workspace.ErrAlreadyUndone
	}
	if expired {
		return true, workspace.ErrExpired
	}
	if err := checkActionSuccessors(ctx, tx, scope, id, data, created, order, source, turnID); err != nil {
		return true, err
	}
	entry := entries[0]
	switch entry.Table {
	case "claim_retirement":
		var before retirementSnapshot
		if json.Unmarshal(entry.Before, &before) != nil {
			return true, memory.ErrInvalid
		}
		current, err := retirementSnapshotTx(ctx, tx, scope.OwnerID, entry.ID)
		if err != nil {
			return true, err
		}
		if retirementFingerprint(current) != entry.AfterHash {
			return true, workspace.ErrChangedSince
		}
		if _, err := tx.Exec(ctx, "UPDATE claims SET retired=$3,retired_by=$4,retired_at=$5,compared=$6 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), entry.ID, before.Retired, before.By, before.At, before.Compared); err != nil {
			return true, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM background_markers WHERE owner_id=$1 AND record_id=$2 AND (stage LIKE 'memory.compare:%:restored:'||$3 OR stage LIKE 'memory.compare_restored:%:'||$3)", string(scope.OwnerID), entry.ID, id); err != nil {
			return true, err
		}
	case "entity_merge":
		var snapshot entityMergeSnapshot
		if json.Unmarshal(entry.Before, &snapshot) != nil {
			return true, memory.ErrInvalid
		}
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(scope.OwnerID)+":entities"); err != nil {
			return true, err
		}
		for _, ref := range []memory.Ref{snapshot.Merged, snapshot.Kept} {
			var v int
			if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR UPDATE", string(scope.OwnerID), string(ref.ID)).Scan(&v); err != nil || v != ref.Version {
				return true, workspace.ErrChangedSince
			}
		}
		hash, err := entityMergeFingerprintTx(ctx, tx, scope.OwnerID, snapshot)
		if err != nil {
			return true, err
		}
		if hash != entry.AfterHash {
			return true, workspace.ErrChangedSince
		}
		for _, c := range snapshot.Subjects {
			if _, err := tx.Exec(ctx, "UPDATE claim_revisions SET subject_id=$4 WHERE owner_id=$1 AND claim_id=$2 AND version=$3", string(scope.OwnerID), c.ID, c.Version, string(snapshot.Merged.ID)); err != nil {
				return true, err
			}
		}
		for _, c := range snapshot.Mentions {
			if !c.KeptBefore {
				if _, err := tx.Exec(ctx, "DELETE FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$3 AND role=$4 AND entity_id=$5", string(scope.OwnerID), c.ID, c.Version, c.Role, string(snapshot.Kept.ID)); err != nil {
					return true, err
				}
			}
			if _, err := tx.Exec(ctx, "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", string(scope.OwnerID), c.ID, c.Version, string(snapshot.Merged.ID), c.Role); err != nil {
				return true, err
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM aliases WHERE owner_id=$1 AND entity_id=$2 AND entity_version=$3 AND alias=ANY($4::text[])", string(scope.OwnerID), string(snapshot.Kept.ID), snapshot.Kept.Version, snapshot.AddedAliases); err != nil {
			return true, err
		}
		tag, err := tx.Exec(ctx, "UPDATE entity_merges SET undone_at=now() WHERE owner_id=$1 AND merged_id=$2 AND kept_id=$3 AND undone_at IS NULL", string(scope.OwnerID), string(snapshot.Merged.ID), string(snapshot.Kept.ID))
		if err != nil {
			return true, err
		}
		if tag.RowsAffected() != 1 {
			return true, workspace.ErrChangedSince
		}
		// Canonical membership fingerprints determine affected comparison batches;
		// unrelated group completion must survive an identity undo.
	}
	_, err = tx.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
	return true, err
}
