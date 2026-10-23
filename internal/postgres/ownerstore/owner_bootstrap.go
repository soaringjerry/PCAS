package ownerstore

import (
	"context"
	"errors"

	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace/settings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ensureOwner creates the workspace row of an owner and the agents of the
// configured models when they are absent.
func (s *Owners) EnsureOwner(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	defaults := settings.Defaults(workspace.InitialTimezone(ctx))
	if _, err := tx.Exec(ctx, "INSERT INTO workspace_owners(owner_id,settings) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), kernel.JSON(defaults)); err != nil {
		return err
	}
	var models []memory.ModelInfo
	if s.Models != nil {
		models = s.Models.List()
	}
	agents := settings.Agents(models)
	for _, a := range agents {
		if a.ID == "chatgpt-direct" {
			legacy, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id='chatgpt'", string(scope.OwnerID))
			if err == nil {
				a.Enabled = legacy.Enabled
				a.MemoryKinds = legacy.MemoryKinds
				a.IncludeInferred = legacy.IncludeInferred
			} else if !errors.Is(err, memory.ErrNotFound) {
				return err
			}
		}
		tag, err := tx.Exec(ctx, "INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), a.ID, kernel.JSON(a))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 && a.Enabled {
			if err := InitializeAgentMemoriesTx(ctx, tx, scope.OwnerID, a.ID); err != nil {
				return err
			}
		}
		if a.ID == "chatgpt-direct" && tag.RowsAffected() == 1 {
			// The new model channel inherits precisely the existing subscription
			// visibility once. Subsequent explicit grant changes remain authoritative.
			if _, err := tx.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,record_id,'chatgpt-direct' FROM record_grants WHERE owner_id=$1 AND principal_id='chatgpt' ON CONFLICT DO NOTHING", string(scope.OwnerID)); err != nil {
				return err
			}
		}
	}
	// Existing enabled agents are initialized without reopening any grants.
	if _, err := tx.Exec(ctx, `UPDATE workspace_agents SET document=jsonb_set(document,'{memoryInitialized}','true')
        WHERE owner_id=$1 AND coalesce((document->>'enabled')::boolean,false)
        AND NOT coalesce((document->>'memoryInitialized')::boolean,false)`, string(scope.OwnerID)); err != nil {
		return err
	}
	return nil
}

func InitializeAgentMemoriesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, agent string) error {
	_, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id)
        SELECT r.owner_id,r.id,$2 FROM memory_records r JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
        WHERE r.owner_id=$1 AND r.state='active' AND claim_source_is_current($1,c.claim_id,c.version,now())
        AND EXISTS(SELECT 1 FROM record_grants g JOIN workspace_agents ag ON(ag.owner_id,ag.id)=(g.owner_id,g.principal_id)
            WHERE g.owner_id=r.owner_id AND g.record_id=r.id)
        ON CONFLICT DO NOTHING`, string(owner), agent)
	return err
}
