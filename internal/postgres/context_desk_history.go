package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func loadContextArtifactDependenciesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, parentKind, parentID string, parentVersion int) ([]memory.TypedDependency, error) {
	if !oneOf(parentKind, "run", "desk_turn", "artifact", "summary", "manual_package", "training_sample") || parentID == "" || parentVersion < 1 {
		return nil, memory.ErrInvalid
	}
	rows, err := tx.Query(ctx, `SELECT dependency_id::text,dependency_version,dependency_kind,purpose,hard_scope,policy_id::text,policy_revision,scope_revision FROM context_artifact_dependencies WHERE owner_id=$1 AND parent_kind=$2 AND parent_id=$3 AND parent_version=$4 ORDER BY dependency_id,dependency_version,dependency_kind`, string(scope.OwnerID), parentKind, parentID, parentVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memory.TypedDependency{}
	for rows.Next() {
		var d memory.TypedDependency
		var hard []byte
		var policy *string
		var revision *int
		if err := rows.Scan(&d.Ref.ID, &d.Ref.Version, &d.Ref.Kind, &d.Purpose, &hard, &policy, &revision, &d.ScopeRevision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(hard, &d.Scope); err != nil {
			return nil, err
		}
		if policy != nil && revision != nil {
			d.Authorization = &memory.AuthorizationStamp{PolicyID: memory.ID(*policy), Revision: *revision}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Inspect server-owned history. Regrant never revives a scrubbed exchange.
// Original policy revisions are checked before any current-recipient hydrate.
// Returned dependencies describe the current consumer, not a fictional claim
// conversion of source refs. target=nil is an owner history/replay operation.
func (s *Store) deskTurnContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turnID string, target *memory.TrustedTaskContext) ([]memory.TypedDependency, error) {
	if !memory.ID(turnID).Valid() {
		return nil, memory.ErrInvalid
	}
	var agent string
	var legacy []memory.Ref
	var raw []byte
	var thing *string
	var stale bool
	err := tx.QueryRow(ctx, `SELECT agent_id,dependencies,context_task,thing_id::text,coalesce(response->'turn'->>'reply','')='（这条回答依据的记忆已变更）' OR (question='' AND answer='') FROM desk_turns WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), turnID).Scan(&agent, &legacy, &raw, &thing, &stale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, memory.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if stale {
		return nil, memory.ErrConflict
	}
	var stored *memory.TrustedTaskContext
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &stored); err != nil {
			return nil, memory.ErrConflict
		}
	}
	// Capture and deterministic owner policy operations did not dispatch to a
	// model. With no dependencies there is no historical recipient to verify.
	if stored == nil && len(legacy) == 0 {
		return []memory.TypedDependency{}, nil
	}
	var item *workspace.Item
	if thing != nil {
		value, err := getItem(ctx, tx, scope, *thing)
		if err != nil {
			return nil, memory.ErrConflict
		}
		item = &value
	}
	if stored == nil {
		// Legacy Desk producers are secretaries. Build this current read
		// context before the common Run verifier, whose legacy default is a
		// deputy; never persist or invent a historical recipient binding.
		if target == nil {
			task, err := s.trustedTaskContextTx(ctx, tx, scope, agent, "secretary", contextScopeForItem(item), nil)
			if err != nil {
				return nil, err
			}
			target = &task
		}
		modelScope := scope
		if target != nil {
			modelScope.Task = target
			modelScope.PrincipalID = target.Recipient.PrincipalID
			agent = target.Recipient.PrincipalID
		}
		run := workspace.Run{AgentID: agent, ContextVersions: legacy}
		if thing != nil {
			run.ThingID = *thing
		}
		if err = s.verifyRunForItemTx(ctx, tx, modelScope, run, item); err != nil {
			return nil, err
		}
		if len(legacy) == 0 {
			return []memory.TypedDependency{}, nil
		}
		entries, cov, err := hydrateTypedContextTx(ctx, tx, scope, *target, legacy)
		if err != nil {
			return nil, err
		}
		if !cov.Complete || len(entries) != len(legacy) {
			return nil, memory.ErrConflict
		}
		return dependenciesForEntries(entries), nil
	}
	if stored.OwnerID != scope.OwnerID || stored.Recipient.PrincipalID != agent || stored.Scope != contextScopeForItem(item) {
		return nil, memory.ErrConflict
	}
	live, err := s.contextRecipientModelTx(ctx, tx, scope, agent, stored.Recipient.Role, nil, stored.Recipient.Model)
	if err != nil || live != stored.Recipient {
		return nil, memory.ErrConflict
	}
	deps, err := loadContextArtifactDependenciesTx(ctx, tx, scope, "desk_turn", turnID, 1)
	if err != nil {
		return nil, err
	}
	if len(legacy) > 0 && len(deps) == 0 {
		return nil, memory.ErrConflict
	}
	if err = verifyTypedContextTx(ctx, tx, scope, *stored, deps); err != nil {
		return nil, err
	}
	if target == nil {
		return deps, nil
	}
	if target.OwnerID != scope.OwnerID || !target.Recipient.Valid() {
		return nil, memory.ErrForbidden
	}
	entries, cov, err := hydrateTypedContextTx(ctx, tx, scope, *target, refsForDependencies(deps))
	if err != nil {
		return nil, err
	}
	if !cov.Complete || len(entries) != len(deps) {
		return nil, memory.ErrConflict
	}
	return dependenciesForEntries(entries), nil
}

// Keep a verifiable operation receipt while removing stale derived prose and
// item links. Action ownership and this exchange's binding are server facts.
func redactDeskTurnContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turn *workspace.SecretaryTurn) error {
	var erased bool
	var storedResponse []byte
	if err := tx.QueryRow(ctx, "SELECT question='' AND answer='',response FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), turn.ID).Scan(&erased, &storedResponse); err != nil {
		return err
	}
	if erased {
		// Deletion already stored a scrubbed audit skeleton. Keep its empty
		// reply and original operation identity instead of relabeling it as a
		// source revision change or dropping capture receipts without actions.
		if len(storedResponse) > 0 {
			var saved storedSecretaryResponse
			if err := json.Unmarshal(storedResponse, &saved); err != nil {
				return err
			}
			*turn = saved.Turn
			turn.Text, turn.Reply = "", ""
			turn.Cards, turn.Ask = []workspace.DeskCard{}, nil
			for i := range turn.Receipts {
				turn.Receipts[i].Text, turn.Receipts[i].Reason = "（内容已删除）", ""
			}
		} else {
			turn.Text, turn.Reply = "", ""
			turn.Cards, turn.Receipts, turn.Ask = []workspace.DeskCard{}, []workspace.DeskReceipt{}, nil
		}
		return nil
	}
	turn.Reply = "（这条回答依据的记忆已变更）"
	turn.Cards = []workspace.DeskCard{}
	turn.Ask = nil
	kept := []workspace.DeskReceipt{}
	for _, receipt := range turn.Receipts {
		if receipt.ActionID == nil || !memory.ID(*receipt.ActionID).Valid() {
			continue
		}
		var undone bool
		err := tx.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2 AND turn_id=$3", string(scope.OwnerID), *receipt.ActionID, turn.ID).Scan(&undone)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		kept = append(kept, workspace.DeskReceipt{Op: "action", Text: "这项操作已完成；相关回答内容已隐藏", Status: "done", ActionID: receipt.ActionID, Undoable: !undone, Undone: undone})
	}
	turn.Receipts = kept
	return nil
}
