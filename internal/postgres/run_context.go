package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const delegateHistoryTurns = 6
const delegateContextBytes = 4000 // Same query budget as manual handoff; UTF-8-safe recent tail.

type runContextKey struct{}
type preparedRunContext struct {
	Plan     memory.QueryPlan
	Refs     []memory.Ref
	Excerpts []memory.RecallExcerpt
}

type storedDeskContext struct {
	Question, Answer string
	RequestID        string
	Refs             []memory.Ref
	Outdated         bool
}

// Resolve references and perform semantic retrieval before Execute takes the
// owner lock. All selected versions and current documents are rechecked inside
// the transaction; this snapshot never grants access by itself.
func (s *Store) prepareRunContext(ctx context.Context, scope memory.Scope, c workspace.Command, recall retrieval.QueryEmbed) (context.Context, error) {
	if len(c.DeskTurnIDs) > delegateHistoryTurns {
		return ctx, memory.ErrInvalid
	}
	history := []storedDeskContext{}
	query := c.Prompt
	projectID := c.ProjectID
	var plan memory.QueryPlan
	ready := false
	var thingID string
	if err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		agent, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.AgentID)
		if err != nil {
			return err
		}
		if !agent.Enabled {
			return memory.ErrForbidden
		}
		if c.Type == "delegateTask" && (agent.Channel == "manual" || s.Models == nil || !s.Models.Available(agent.ID)) {
			return memory.ErrUnavailable
		}
		item := workspace.Item{ID: c.ID, Kind: "task", ProjectID: c.ProjectID}
		if c.Type == "requestRun" {
			item, err = itemstore.GetItem(ctx, tx, scope, c.ThingID)
			if err != nil {
				return err
			}
		}
		thingID = item.ID
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		plan = memory.PlanQuery(c.Prompt, s.BusinessNow(), kernel.Location(settings))
		u, err := s.StartUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ready = u.Ready
		history, err = readRunHistoryTx(ctx, tx, scope, c, item)
		if err != nil {
			return err
		}
		if c.Type == "requestRun" {
			item, _, err = accessstore.SanitizeItemTx(ctx, tx, scope, c.AgentID, item)
			if err != nil {
				return err
			}
			projectID = item.ProjectID
			if item.Kind == "project" {
				projectID = item.ID
			}
			query += " " + item.Title + " " + item.Notes + " " + item.Body + " " + item.Goal
			docs, _, err := currentRunDocsTx(ctx, tx, scope, item, c.AgentID)
			if err != nil {
				return err
			}
			for _, doc := range docs {
				query += " " + doc.Title + " " + usestore.Tail(doc.Body, delegateContextBytes)
			}
		}
		query = runHistoryQuery(query, history, c.Prompt)
		if omitted := len(query) - len(usestore.Tail(query, delegateContextBytes)); omitted > 0 {
			if err := kernel.StageEvent(ctx, tx, scope.OwnerID, "deputy", "overflow", "retrieval_query_bytes", omitted); err != nil {
				return err
			}
		}
		query = usestore.Tail(query, delegateContextBytes)
		return nil
	}); err != nil {
		return ctx, err
	}
	request := memory.RecallRequest{Team: &memory.TeamRecall{Text: query, Plan: plan, ThingID: &thingID, ProjectID: &projectID, RankFusion: ready}, Query: query, Mode: memory.Continue, Budget: memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}}
	if projectID != "" {
		request.Context.Objects = []memory.ID{memory.ID(projectID)}
	}
	result, err := s.RecallWith(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.AgentID, Team: true}, request, recall)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, runContextKey{}, preparedRunContext{Plan: plan, Refs: result.Memories, Excerpts: result.Excerpts}), nil
}

// Documents are read again inside runCommandTx. A prepared retrieval snapshot
// must never override the owner's current writing or the destination's grants.
func currentRunDocsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, agent string) ([]workspace.Doc, []memory.Ref, error) {
	project := item.ProjectID
	if item.Kind == "project" {
		project = item.ID
	}
	docs, err := kernel.QueryDocuments[workspace.Doc](ctx, tx, "SELECT d.document FROM work_documents d JOIN work_items w ON(w.owner_id,w.id)=(d.owner_id,d.thing_id) WHERE d.owner_id=$1 AND (w.id=$2 OR w.id=nullif($3,'')::uuid OR w.project_id=nullif($3,'')::uuid) ORDER BY d.document->>'updatedAt' DESC,d.id", string(scope.OwnerID), item.ID, project)
	if err != nil {
		return nil, nil, err
	}
	permitted := []workspace.Doc{}
	refs := []memory.Ref{}
	for _, doc := range docs {
		if doc.RunID != "" {
			run, err := kernel.QueryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.RunID)
			if err != nil {
				if err == memory.ErrNotFound {
					continue
				}
				return nil, nil, err
			}
			run.AgentID = agent
			if accessstore.VerifyRunForItemTx(ctx, tx, scope, run, &item) != nil {
				continue
			}
			refs = append(refs, run.ContextVersions...)
		}
		permitted = append(permitted, doc)
	}
	return permitted, kernel.UniqueRefs(refs), nil
}

// Automatic history is constrained to the server-selected conversation. Manual
// IDs retain their existing owner/agent boundary. Re-read under the owner lock
// so an earlier prepared snapshot cannot resurrect an erased answer.
func readRunHistoryTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command, item workspace.Item) ([]storedDeskContext, error) {
	if len(c.DeskTurnIDs) > delegateHistoryTurns {
		return nil, memory.ErrInvalid
	}
	automatic, auto := ctx.Value(kernel.DelegateHistoryKey{}).(intakestore.DelegateHistoryContext)
	history := []storedDeskContext{}
	for _, id := range c.DeskTurnIDs {
		if !memory.ID(id).Valid() {
			return nil, memory.ErrInvalid
		}
		// The team shares the owner's conversation: a manual handoff may carry
		// turns held with any agent, as the automatic one does.
		boundary := "$3::text=$3::text"
		target := c.AgentID
		if auto {
			if !kernel.OneOf(id, automatic.IDs...) {
				return nil, memory.ErrForbidden
			}
			boundary = "conversation_id=$3"
			target = automatic.ConversationID
		}
		var turn storedDeskContext
		if err := tx.QueryRow(ctx, "SELECT question,answer,dependencies,coalesce(request_id::text,'') FROM desk_turns WHERE owner_id=$1 AND id=$2 AND "+boundary, string(scope.OwnerID), id, target).Scan(&turn.Question, &turn.Answer, &turn.Refs, &turn.RequestID); err != nil {
			return nil, memory.ErrNotFound
		}
		if turn.Question == "" && turn.Answer == "" {
			continue
		}
		turn.Outdated = accessstore.VerifyRunForItemTx(ctx, tx, scope, workspace.Run{AgentID: c.AgentID, ContextVersions: turn.Refs}, &item) != nil
		if turn.Outdated {
			turn.Answer = intakestore.OutdatedDeskAnswer
		}
		history = append(history, turn)
	}
	return history, nil
}

func runHistoryText(history []storedDeskContext) string {
	var text strings.Builder
	for _, turn := range history {
		fmt.Fprintf(&text, "\n导办台之前的讨论：\n问：%s\n答：%s\n", turn.Question, turn.Answer)
	}
	return text.String()
}

func runHistoryQuery(base string, history []storedDeskContext, prompt string) string {
	// The latest request stays last when older material exceeds the query budget.
	return strings.TrimSpace(base + runHistoryText(history) + "\n当前要求：" + prompt)
}
