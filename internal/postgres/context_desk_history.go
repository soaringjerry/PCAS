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
	var item *workspace.Item
	if thing != nil {
		value, err := getItem(ctx, tx, scope, *thing)
		if err != nil {
			return nil, memory.ErrConflict
		}
		item = &value
	}
	if stored == nil {
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
		if target == nil {
			task, err := s.trustedTaskContextTx(ctx, tx, scope, agent, "secretary", contextScopeForItem(item), nil)
			if err != nil {
				return nil, err
			}
			target = &task
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
