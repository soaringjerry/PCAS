package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type runContextKey struct{}
type preparedRunContext struct {
	Refs     []memory.Ref
	Previous *workspace.Run
	History  []storedDeskContext
}

type storedDeskContext struct {
	Question, Answer string
	Refs             []memory.Ref
}

// Resolve references and perform semantic retrieval before Execute takes the
// owner lock. All selected versions and previous outputs are rechecked inside
// the transaction; this snapshot never grants access by itself.
func (s *Store) prepareRunContext(ctx context.Context, scope memory.Scope, c workspace.Command) (context.Context, error) {
	if len(c.DeskTurnIDs) > 6 {
		return ctx, memory.ErrInvalid
	}
	history := []storedDeskContext{}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.AgentID)
		if err != nil {
			return err
		}
		if !agent.Enabled {
			return memory.ErrForbidden
		}
		if c.Type == "delegateTask" && (agent.Channel == "manual" || s.models == nil || !s.models.Available(agent.ID)) {
			return memory.ErrUnavailable
		}
		for _, id := range c.DeskTurnIDs {
			if !memory.ID(id).Valid() {
				return memory.ErrInvalid
			}
			var turn storedDeskContext
			if err := tx.QueryRow(ctx, "SELECT question,answer,dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2 AND agent_id=$3", string(scope.OwnerID), id, c.AgentID).Scan(&turn.Question, &turn.Answer, &turn.Refs); err != nil {
				return memory.ErrNotFound
			}
			if verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: c.AgentID, ContextVersions: turn.Refs}) == nil {
				history = append(history, turn)
			}
		}
		return nil
	}); err != nil {
		return ctx, err
	}
	query := c.Prompt
	var projectID string
	var previous *workspace.Run
	if c.Type == "requestRun" {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			item, err := getItem(ctx, tx, scope, c.ThingID)
			if err != nil {
				return err
			}
			item, _, err = sanitizeItemTx(ctx, tx, scope, c.AgentID, item)
			if err != nil {
				return err
			}
			projectID = item.ProjectID
			if item.Kind == "project" {
				projectID = item.ID
			}
			query += " " + item.Title + " " + item.Notes + " " + item.Body + " " + item.Goal + " " + item.Progress
			runs, err := queryDocuments[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 AND agent_id=$3 AND status='done' ORDER BY created_at DESC LIMIT 1", string(scope.OwnerID), item.ID, c.AgentID)
			if err != nil {
				return err
			}
			if len(runs) > 0 && !runs[0].StaleContext && verifyRunTx(ctx, tx, scope, runs[0]) == nil {
				previous = &runs[0]
				query += " " + previous.Prompt + " " + previous.Output
			}
			return nil
		})
		if err != nil {
			return ctx, err
		}
	}
	request := memory.RecallRequest{Query: tail(strings.TrimSpace(query), 4000), Mode: memory.Continue, Budget: memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}}
	if projectID != "" {
		request.Context.Objects = []memory.ID{memory.ID(projectID)}
	}
	result, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.AgentID}, request)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, runContextKey{}, preparedRunContext{Refs: result.Memories, Previous: previous, History: history}), nil
}
