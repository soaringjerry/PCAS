package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type runContextKey struct{}
type preparedRunContext struct {
	Plan     memory.QueryPlan
	Refs     []memory.Ref
	Excerpts []memory.RecallExcerpt
	History  []storedDeskContext
}

type storedDeskContext struct {
	Question, Answer string
	RequestID        string
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
	var plan memory.QueryPlan
	var thingID string
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
		thingID = item.ID
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		plan = memory.PlanQuery(c.Prompt, time.Now(), deskLocation(settings))
		for _, id := range c.DeskTurnIDs {
			if !memory.ID(id).Valid() {
				return memory.ErrInvalid
			}
			var turn storedDeskContext
			if err := tx.QueryRow(ctx, "SELECT question,answer,dependencies,coalesce(request_id::text,'') FROM desk_turns WHERE owner_id=$1 AND id=$2 AND agent_id=$3", string(scope.OwnerID), id, c.AgentID).Scan(&turn.Question, &turn.Answer, &turn.Refs, &turn.RequestID); err != nil {
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
			docs, _, err := currentRunDocsTx(ctx, tx, scope, item, c.AgentID)
			if err != nil {
				return err
			}
			for _, doc := range docs {
				query += " " + doc.Title + " " + tail(doc.Body, 4000)
			}
		}
		return nil
	}); err != nil {
		return ctx, err
	}
	request := memory.RecallRequest{Team: &memory.TeamRecall{Text: c.Prompt, Plan: plan, ThingID: &thingID, ProjectID: &projectID}, Query: tail(strings.TrimSpace(query), 4000), Mode: memory.Continue, Budget: memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}}
	if projectID != "" {
		request.Context.Objects = []memory.ID{memory.ID(projectID)}
	}
	result, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.AgentID, Team: true}, request)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, runContextKey{}, preparedRunContext{Plan: plan, Refs: result.Memories, Excerpts: result.Excerpts, History: history}), nil
}

// Documents are read again inside runCommandTx. A prepared retrieval snapshot
// must never override the owner's current writing or the destination's grants.
func currentRunDocsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, agent string) ([]workspace.Doc, []memory.Ref, error) {
	docs, err := queryDocuments[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND thing_id=$2 ORDER BY document->>'updatedAt' DESC,id", string(scope.OwnerID), item.ID)
	if err != nil {
		return nil, nil, err
	}
	permitted := []workspace.Doc{}
	refs := []memory.Ref{}
	for _, doc := range docs {
		if doc.RunID != "" {
			run, err := queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.RunID)
			if err != nil {
				if err == memory.ErrNotFound {
					continue
				}
				return nil, nil, err
			}
			run.AgentID = agent
			if verifyRunForItemTx(ctx, tx, scope, run, &item) != nil {
				continue
			}
			refs = append(refs, run.ContextVersions...)
		}
		permitted = append(permitted, doc)
	}
	return permitted, uniqueRefs(refs), nil
}
