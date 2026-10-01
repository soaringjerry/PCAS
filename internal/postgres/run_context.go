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
	Outdated         bool
}

// Resolve references and perform semantic retrieval before Execute takes the
// owner lock. All selected versions and previous outputs are rechecked inside
// the transaction; this snapshot never grants access by itself.
func (s *Store) prepareRunContext(ctx context.Context, scope memory.Scope, c workspace.Command) (context.Context, error) {
	if len(c.DeskTurnIDs) > 6 {
		return ctx, memory.ErrInvalid
	}
	history := []storedDeskContext{}
	query := c.Prompt
	projectID := c.ProjectID
	var previous *workspace.Run
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
		item := workspace.Item{ID: c.ID, Kind: "task", ProjectID: c.ProjectID}
		if c.Type == "requestRun" {
			item, err = getItem(ctx, tx, scope, c.ThingID)
			if err != nil {
				return err
			}
		}
		for _, id := range c.DeskTurnIDs {
			if !memory.ID(id).Valid() {
				return memory.ErrInvalid
			}
			var turn storedDeskContext
			if err := tx.QueryRow(ctx, "SELECT question,answer,dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2 AND agent_id=$3", string(scope.OwnerID), id, c.AgentID).Scan(&turn.Question, &turn.Answer, &turn.Refs); err != nil {
				return memory.ErrNotFound
			}
			if turn.Question != "" || turn.Answer != "" {
				turn.Outdated = verifyRunForItemTx(ctx, tx, scope, workspace.Run{AgentID: c.AgentID, ContextVersions: turn.Refs}, &item) != nil
				history = append(history, turn)
			}
		}
		if c.Type == "requestRun" {
			item, _, err = sanitizeItemTx(ctx, tx, scope, c.AgentID, item)
			if err != nil {
				return err
			}
			projectID = item.ProjectID
			if item.Kind == "project" {
				projectID = item.ID
			}
			query += " " + item.Title + " " + item.Notes + " " + item.Body + " " + item.Goal + " " + item.Progress
			previous, err = mostRecentPermittedRunTx(ctx, tx, scope, item, c.AgentID)
			if err != nil {
				return err
			}
			if previous != nil {
				query += " " + previous.Prompt + " " + previous.Output
			}
		}
		return nil
	}); err != nil {
		return ctx, err
	}
	request := memory.RecallRequest{Query: tail(strings.TrimSpace(query), 4000), Mode: memory.Continue, Budget: memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}}
	if projectID != "" {
		request.Context.Objects = []memory.ID{memory.ID(projectID)}
	}
	result, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.AgentID, Team: true}, request)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, runContextKey{}, preparedRunContext{Refs: result.Memories, Previous: previous, History: history}), nil
}

func mostRecentPermittedRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, agent string) (*workspace.Run, error) {
	// Bound each page's memory use, not how far back an authorized result can
	// be found. Revoking the newest answer must not discard ordinary history.
	const pageSize = 20
	for offset := 0; ; offset += pageSize {
		runs, err := queryDocuments[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 AND agent_id=$3 AND status='done' ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5", string(scope.OwnerID), item.ID, agent, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if !run.StaleContext && verifyRunForItemTx(ctx, tx, scope, run, &item) == nil {
				return &run, nil
			}
		}
		if len(runs) < pageSize {
			return nil, nil
		}
	}
}
