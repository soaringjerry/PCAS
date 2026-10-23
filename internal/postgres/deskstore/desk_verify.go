package deskstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Input content may change during generation. Access revocation and deletion
// remain fences; use current versions so unrelated edits do not abort a reply.
func (s *Desk) checkSecretaryUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretary.Context) error {
	agent, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.Agent.ID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	var thing string
	if item, ok := c.Aliases["THIS"]; ok {
		thing = item.ID
	}
	if err := accessstore.VerifyRunAccessTx(ctx, usestore.UseClockTx{Tx: tx, At: time.Now()}, scope, workspace.Run{ThingID: thing, AgentID: c.Agent.ID, ContextVersions: c.Dependencies}); err != nil {
		return fmt.Errorf("%w: input access changed", memory.ErrForbidden)
	}
	return nil
}

func (s *Desk) checkSecretaryActionTargetsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretary.Context, out SecretaryOutput) error {
	if err := s.checkSecretaryUseContextTx(ctx, tx, scope, c); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, action := range out.Actions {
		if action.SelfcheckDropped {
			continue
		}
		if action.Op == "delegate" && action.Kind == "revise" {
			doc, ok := c.Documents[PointerValue(action.DocumentID)]
			if ok {
				current, e := kernel.QueryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.ID)
				if e != nil || current.Version != doc.Version {
					return memory.ErrConflict
				}
			}
		}
		refs := []string{action.Ref}
		if action.Project != nil {
			refs = append(refs, *action.Project)
		}
		if project, ok := secretary.TextField(action.Set, "project"); ok {
			refs = append(refs, project)
		}
		for _, ref := range refs {
			sent, ok := c.Aliases[ref]
			if !ok || seen[sent.ID] {
				continue
			}
			seen[sent.ID] = true
			current, err := itemstore.GetItem(ctx, tx, scope, sent.ID)
			if err != nil {
				return memory.ErrConflict
			}
			// Compare the target's stored document, independent of changes to
			// memory provenance used to sanitize the model's input.
			if original, ok := c.TargetItems[sent.ID]; ok {
				sent = original
			}

			if string(kernel.JSON(current)) != string(kernel.JSON(sent)) {
				return memory.ErrConflict
			}
		}
	}
	return nil
}
