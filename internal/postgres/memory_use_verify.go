package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Input content may change during generation. Access revocation and deletion
// remain fences; use current versions so unrelated edits do not abort a reply.
func (s *Store) checkSecretaryUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretaryContext) error {
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.Agent.ID)
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
	if err := verifyRunAccessTx(ctx, useClockTx{Tx: tx, at: time.Now()}, scope, workspace.Run{ThingID: thing, AgentID: c.Agent.ID, ContextVersions: c.Dependencies}); err != nil {
		return fmt.Errorf("%w: input access changed", memory.ErrForbidden)
	}
	return nil
}
func (s *Store) checkSecretaryActionTargetsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretaryContext, out secretaryOutput) error {
	if err := s.checkSecretaryUseContextTx(ctx, tx, scope, c); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, action := range out.Actions {
		if action.selfcheckDropped {
			continue
		}
		if action.Op == "delegate" && action.Kind == "revise" {
			doc, ok := c.Documents[pointerValue(action.DocumentID)]
			if ok {
				current, e := queryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.ID)
				if e != nil || current.Version != doc.Version {
					return memory.ErrConflict
				}
			}
		}
		refs := []string{action.Ref}
		if action.Project != nil {
			refs = append(refs, *action.Project)
		}
		if project, ok := textField(action.Set, "project"); ok {
			refs = append(refs, project)
		}
		for _, ref := range refs {
			sent, ok := c.Aliases[ref]
			if !ok || seen[sent.ID] {
				continue
			}
			seen[sent.ID] = true
			current, err := getItem(ctx, tx, scope, sent.ID)
			if err != nil {
				return memory.ErrConflict
			}
			// Compare the target's stored document, independent of changes to
			// memory provenance used to sanitize the model's input.
			if original, ok := c.TargetItems[sent.ID]; ok {
				sent = original
			}

			if string(asJSON(current)) != string(asJSON(sent)) {
				return memory.ErrConflict
			}
		}
	}
	return nil
}

// PostgreSQL now() is frozen at transaction start. The ordered turn keeps its
// transaction across the model call, so completion checks must see revisions
// committed during generation. Only this read-only verifier uses wall time.
type useClockTx struct {
	pgx.Tx
	at time.Time
}

func (tx useClockTx) queryAt(sql string, args []any) (string, []any) {
	if !strings.Contains(sql, "now()") {
		return sql, args
	}
	args = append(append([]any{}, args...), tx.at)
	return strings.ReplaceAll(sql, "now()", fmt.Sprintf("$%d::timestamptz", len(args))), args
}
func (tx useClockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = tx.queryAt(sql, args)
	return tx.Tx.Query(ctx, sql, args...)
}
func (tx useClockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = tx.queryAt(sql, args)
	return tx.Tx.QueryRow(ctx, sql, args...)
}

// A queued prompt has not been sent yet. Preserve the existing access/currentness
// fence before the first paid call; later input changes do not discard a draft.
func checkQueuedUseRunPromptTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	if err := verifyRunTx(ctx, tx, scope, run); err != nil {
		return err
	}
	ids := []string{}
	for _, ref := range run.ContextVersions {
		if ref.Kind != memory.SourceKind {
			ids = append(ids, string(ref.ID))
		}
	}
	var retired bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND coalesce(to_jsonb(claims)->>'retired','')!='')", string(scope.OwnerID), ids).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return memory.ErrConflict
	}
	return checkUseRunPromptTx(ctx, tx, scope, run)
}

// A run writes its destination item. Other input memories may change while
// its paid draft is generated; their revision is not an abort condition.
func checkUseRunPromptTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	item, err := getItem(ctx, tx, scope, run.ThingID)
	if err != nil {
		return err
	}
	if run.TargetHash != "" && itemHash(item) != run.TargetHash {
		return memory.ErrConflict
	}
	return nil
}
func itemHash(item workspace.Item) string {
	// Provenance and audit writes in the admitting turn do not change the
	// destination's business content or invalidate an otherwise current prompt.
	item.Version = 0
	item.Sources = nil
	item.History = nil
	item.Evolution = nil
	item.CreatedAt = ""
	item.UpdatedAt = ""
	item.HasRetainedWriting = false
	return fmt.Sprintf("%x", sha256.Sum256(asJSON(item)))
}

// A changed destination or lost worker lease prevents further model work.
func (s *Store) checkDeputySelfcheckContext(ctx context.Context, scope memory.Scope, run workspace.Run, token string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var leased bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND id=$2 AND status='running' AND lease_token=$3 AND lease_until>now())", string(scope.OwnerID), run.ID, token).Scan(&leased); err != nil {
			return err
		}
		if !leased {
			return memory.ErrConflict
		}
		if err := checkUseRunPromptTx(ctx, tx, scope, run); err != nil {
			return err
		}
		return verifyRunAccessTx(ctx, useClockTx{Tx: tx, at: time.Now()}, scope, run)
	})
}

// Context-derived events refer to an identity already supplied to the model.
// A concurrent revision can update that identity without invalidating the answer.
func recordContextUseTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, event memory.UseEvent) error {
	if event.Kind != "user_mention" && event.Kind != "adoption" {
		return memory.ErrInvalid
	}
	var current int
	err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active'", string(scope.OwnerID), string(event.Ref.ID)).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	event.Ref.Version = current
	return recordUseTx(ctx, tx, scope, event)
}
