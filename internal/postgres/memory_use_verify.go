package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Input memories may change during generation. Only agent availability is a
// precondition for producing an answer; final checks cover actual write targets.
func (s *Store) checkSecretaryUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretaryContext) error {
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.Agent.ID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
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
func itemHash(item workspace.Item) string { return fmt.Sprintf("%x", sha256.Sum256(asJSON(item))) }

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
		return checkUseRunPromptTx(ctx, tx, scope, run)
	})
}
