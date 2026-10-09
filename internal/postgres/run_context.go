package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const delegateHistoryTurns = 6
const delegateContextBytes = 4000 // Same query budget as manual handoff; UTF-8-safe recent tail.

type delegateHistoryKey struct{}
type delegateHistoryContext struct {
	ConversationID string
	IDs            []string
}

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
func (s *Store) prepareRunContext(ctx context.Context, scope memory.Scope, c workspace.Command) (context.Context, error) {
	if len(c.DeskTurnIDs) > delegateHistoryTurns {
		return ctx, memory.ErrInvalid
	}
	history := []storedDeskContext{}
	queryRefs := []memory.Ref{}
	query := c.Prompt
	projectID := c.ProjectID
	var plan memory.QueryPlan
	ready := false
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
		plan = memory.PlanQuery(c.Prompt, s.businessNow(), deskLocation(settings))
		u, err := s.startUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ready = u.Ready
		history, err = readRunHistoryTx(ctx, tx, scope, c, item)
		if err != nil {
			return err
		}
		historyRequests := []string{}
		for _, turn := range history {
			if turn.RequestID != "" {
				historyRequests = append(historyRequests, turn.RequestID)
			}
			if !turn.Outdated {
				queryRefs = append(queryRefs, turn.Refs...)
			}
		}
		originals, err := queryHistorySourceRefsTx(ctx, tx, scope.OwnerID, nil, historyRequests)
		if err != nil {
			return err
		}
		queryRefs = append(queryRefs, originals...)
		if c.Type == "requestRun" {
			var itemRefs []memory.Ref
			item, itemRefs, err = sanitizeItemTx(ctx, tx, scope, c.AgentID, item)
			if err != nil {
				return err
			}
			queryRefs = append(queryRefs, itemRefs...)
			projectID = item.ProjectID
			if item.Kind == "project" {
				projectID = item.ID
			}
			query += " " + item.Title + " " + item.Notes + " " + item.Body + " " + item.Goal
			docs, docRefs, err := currentRunDocsTx(ctx, tx, scope, item, c.AgentID)
			if err != nil {
				return err
			}
			queryRefs = append(queryRefs, docRefs...)
			for _, doc := range docs {
				query += " " + doc.Title + " " + tail(doc.Body, delegateContextBytes)
			}
		}
		query = runHistoryQuery(query, history, c.Prompt)
		if omitted := len(query) - len(tail(query, delegateContextBytes)); omitted > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", "retrieval_query_bytes", omitted); err != nil {
				return err
			}
		}
		query = tail(query, delegateContextBytes)
		return nil
	}); err != nil {
		return ctx, err
	}
	request := memory.RecallRequest{Team: &memory.TeamRecall{Text: query, Plan: plan, ThingID: &thingID, ProjectID: &projectID, RankFusion: ready}, Query: query, Mode: memory.Continue, Budget: memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}}
	if projectID != "" {
		request.Context.Objects = []memory.ID{memory.ID(projectID)}
	}
	queryCtx := context.WithValue(ctx, recallCommandOriginKey{}, c.RequestID)
	queryCtx = context.WithValue(queryCtx, recallInputRefsKey{}, uniqueRefs(queryRefs))
	result, err := s.Recall(queryCtx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.AgentID, Team: true}, request)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, runContextKey{}, preparedRunContext{Plan: plan, Refs: result.Memories, Excerpts: result.Excerpts}), nil
}

// Original desk/capture input uses external version "1". Resolve that stored
// version instead of claiming that later source edits supplied the old question.
func queryHistorySourceRefsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, turnIDs, requestIDs []string) ([]memory.Ref, error) {
	if len(turnIDs) == 0 && len(requestIDs) == 0 {
		return nil, nil
	}
	return queryDocuments[memory.Ref](ctx, tx, `SELECT jsonb_build_object('kind','source','id',s.id,'version',v.version)
 FROM desk_turns t JOIN sources s ON s.owner_id=t.owner_id AND lower(s.external_id)=t.request_id::text
 JOIN source_versions v ON(v.owner_id,v.source_id)=(s.owner_id,s.id) AND v.external_version='1'
 JOIN memory_records r ON(r.owner_id,r.id)=(s.owner_id,s.id) AND r.state='active'
 WHERE t.owner_id=$1 AND s.connector IN ('desk','capture','desk-incomplete')
 AND (t.id=ANY($2::uuid[]) OR t.request_id::text=ANY($3::text[])) ORDER BY s.id,v.version`, string(owner), turnIDs, requestIDs)
}

// Documents are read again inside runCommandTx. A prepared retrieval snapshot
// must never override the owner's current writing or the destination's grants.
func currentRunDocsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, agent string) ([]workspace.Doc, []memory.Ref, error) {
	project := item.ProjectID
	if item.Kind == "project" {
		project = item.ID
	}
	docs, err := queryDocuments[workspace.Doc](ctx, tx, "SELECT d.document FROM work_documents d JOIN work_items w ON(w.owner_id,w.id)=(d.owner_id,d.thing_id) WHERE d.owner_id=$1 AND (w.id=$2 OR w.id=nullif($3,'')::uuid OR w.project_id=nullif($3,'')::uuid) ORDER BY d.document->>'updatedAt' DESC,d.id", string(scope.OwnerID), item.ID, project)
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

// Automatic history is constrained to the server-selected conversation. Manual
// IDs retain their existing owner/agent boundary. Re-read under the owner lock
// so an earlier prepared snapshot cannot resurrect an erased answer.
func readRunHistoryTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command, item workspace.Item) ([]storedDeskContext, error) {
	if len(c.DeskTurnIDs) > delegateHistoryTurns {
		return nil, memory.ErrInvalid
	}
	automatic, auto := ctx.Value(delegateHistoryKey{}).(delegateHistoryContext)
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
			if !oneOf(id, automatic.IDs...) {
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
		turn.Outdated = verifyRunForItemTx(ctx, tx, scope, workspace.Run{AgentID: c.AgentID, ContextVersions: turn.Refs}, &item) != nil
		if turn.Outdated {
			turn.Answer = outdatedDeskAnswer
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
