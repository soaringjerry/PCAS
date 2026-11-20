package deputy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// HistoryTurns is the most secretary turns that one run is handed.
const HistoryTurns = 6

// HistoryBytes is the size, in bytes, of the retrieval query of a run and of
// the history in its brief. It is the budget of a manual handoff. The recent
// end is kept, so the request stays in the query.
const HistoryBytes = 4000

// queryTerms is the most search terms of the retrieval query of a run. The
// recent terms are kept.
const queryTerms = 120

// Source excerpts of a run: at most this many segments and characters.
const (
	sourceSegments   = 8
	sourceCharacters = 4000
)

// recallBudget is the retrieval budget of a run.
var recallBudget = memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}

// Turn is an earlier secretary turn that a run is handed. Outdated is true
// when the agent may no longer use what the answer used; the answer is then
// a notice.
type Turn struct {
	Question, Answer string
	RequestID        string
	Refs             []memory.Ref
	Outdated         bool
}

// Prepared is the retrieval of a run that ran before the owner's lock was
// taken. The request checks again each memory and document that it uses.
type Prepared struct {
	Plan     memory.QueryPlan
	Refs     []memory.Ref
	Excerpts []memory.RecallExcerpt
}

// HistoryText is the history of a run as its brief holds it.
func HistoryText(history []Turn) string {
	var text strings.Builder
	for _, turn := range history {
		fmt.Fprintf(&text, "\n导办台之前的讨论：\n问：%s\n答：%s\n", turn.Question, turn.Answer)
	}
	return text.String()
}

// HistoryQuery is the retrieval query of a run before its budget: base, the
// history, and the request last.
func HistoryQuery(base string, history []Turn, prompt string) string {
	return strings.TrimSpace(base + HistoryText(history) + "\n当前要求：" + prompt)
}

// PreparedQuery is the query of the retrieval before the lock: the request,
// the item, the current documents, and the history. Each document gives its
// recent end.
func PreparedQuery(prompt string, item *workspace.Item, docs []workspace.Doc, history []Turn) string {
	query := prompt
	if item != nil {
		query += " " + item.Title + " " + item.Notes + " " + item.Body + " " + item.Goal
		for _, doc := range docs {
			query += " " + doc.Title + " " + contextbuilder.Tail(doc.Body, HistoryBytes)
		}
	}
	return HistoryQuery(query, history, prompt)
}

// RequestUnit is the storage of one run request, in the transaction of the
// request. Each method reads or writes; none decides.
type RequestUnit interface {
	Item(ctx context.Context, id string) (workspace.Item, error)
	Agent(ctx context.Context, id string) (workspace.Agent, error)
	// Sanitize is the item as the agent may see it, and the derived copies
	// that it holds.
	Sanitize(ctx context.Context, agent string, item workspace.Item) (workspace.Item, []memory.Ref, error)
	// History reads the turns that the command hands the run.
	History(ctx context.Context, c workspace.Command, item workspace.Item) ([]Turn, error)
	// Memories are the current memories that the agent may read.
	Memories(ctx context.Context, agent string) ([]workspace.Memory, error)
	// Smoke is the smoke check of the request, or empty.
	Smoke(ctx context.Context) string
	// Documents are the current documents of the item and its project that
	// the agent may read, and the memories that they were written from.
	Documents(ctx context.Context, item workspace.Item, agent string) ([]workspace.Doc, []memory.Ref, error)
	Document(ctx context.Context, id string) (workspace.Doc, error)
	DocumentVersion(ctx context.Context, id string, version int) (workspace.DocumentVersion, error)
	Settings(ctx context.Context) (workspace.Settings, error)
	StartUse(ctx context.Context) (contextbuilder.UseContext, error)
	// Recall runs the team retrieval of the agent.
	Recall(ctx context.Context, agent string, request memory.RecallRequest, budget memory.Budget, tokens []string) (memory.RecallResult, error)
	// Tier is the memory tier that the caller asked for, or empty.
	Tier(ctx context.Context) string
	FinishUse(ctx context.Context, agent workspace.Agent, item, prompt string, ranked []workspace.Memory, u *contextbuilder.UseContext) error
	Sources(ctx context.Context, agent, item string, excerpts []memory.RecallExcerpt, historyRequests []string, segments, characters int) ([]memory.RecallExcerpt, error)
	// Evidence reads the source context of the claims. batch reads the
	// anchors of the memory use in one query.
	Evidence(ctx context.Context, agent, item string, claims []contextbuilder.Claim, batch bool) ([]contextbuilder.EvidenceGroup, []string, error)
	// Overflow records the count of what did not fit, by reason.
	Overflow(ctx context.Context, reason string, count int) error
	Available(agent string) bool
	Estimate(agent, input string) float64
	// SpentSince is what runs and background work reserved since start.
	SpentSince(ctx context.Context, start time.Time) (float64, error)
	// Insert writes the run, its revision target, and its dependencies.
	Insert(ctx context.Context, run workspace.Run, status string, reviseTarget int) error
}

// Request makes the run that a requestRun command asks for. It writes the
// brief of the run and reserves its cost. prepared is the retrieval before
// the lock, or nil. business is the business time; now is the execution
// time, for the identity of the run and its budget day.
func Request(ctx context.Context, u RequestUnit, c workspace.Command, prepared *Prepared, business, now time.Time) error {
	if err := CheckRequest(c.Kind, c.Prompt); err != nil {
		return err
	}
	item, err := u.Item(ctx, c.ThingID)
	if err != nil {
		return err
	}
	agent, err := u.Agent(ctx, c.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	targetHash := workspace.ContentHash(item)
	item, artifactRefs, err := u.Sanitize(ctx, agent.ID, item)
	if err != nil {
		return err
	}
	if len(c.DeskTurnIDs) > HistoryTurns {
		return memory.ErrInvalid
	}
	history, err := u.History(ctx, c, item)
	if err != nil {
		return err
	}
	id := c.ID
	if id == "" {
		id = string(memory.NewID())
	} else if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	memories, err := u.Memories(ctx, agent.ID)
	if err != nil {
		return err
	}

	run := workspace.Run{DocumentVersions: []workspace.RunDocumentVersion{}, DocumentID: c.DocumentID, BaseVersion: c.BaseVersion, TargetHash: targetHash, SmokeID: u.Smoke(ctx), ID: id, ThingID: item.ID, AgentID: agent.ID, Kind: c.Kind, Prompt: c.Prompt, Status: "running", ContextMemoryIDs: []string{}, ContextVersions: []memory.Ref{}, CreatedAt: now.UTC().Format(time.RFC3339Nano)}
	var brief Brief
	projectID := item.ProjectID
	var project *workspace.Item
	if item.Kind == "project" {
		projectID = item.ID
	} else if projectID != "" {
		found, err := u.Item(ctx, projectID)
		if err != nil {
			return err
		}
		project = &found
	}
	brief.Item(item, project)
	docs, docRefs, err := u.Documents(ctx, item, agent.ID)
	if err != nil {
		return err
	}

	var reviseTarget int
	reviseID := ""
	if c.Kind == "revise" {
		if !memory.ID(c.DocumentID).Valid() || c.BaseVersion < 1 {
			return memory.ErrInvalid
		}
		target, err := u.Document(ctx, c.DocumentID)
		if err != nil {
			return err
		}
		if target.ThingID != item.ID {
			return memory.ErrInvalid
		}
		reviseTarget = target.Version
		base, err := u.DocumentVersion(ctx, c.DocumentID, c.BaseVersion)
		if err != nil {
			return err
		}
		brief.ReviseBase(c.DocumentID, c.BaseVersion, base.Title, base.Body)
		reviseID = c.DocumentID
	}
	if err := u.Overflow(ctx, "document_char_budget", brief.CurrentDocuments(docs, reviseID)); err != nil {
		return err
	}
	run.DocumentVersions = append(run.DocumentVersions, brief.Documents...)
	artifactRefs = append(artifactRefs, docRefs...)
	settings, err := u.Settings(ctx)
	if err != nil {
		return err
	}
	loc := settings.Location()
	plan := memory.PlanQuery(c.Prompt, business, loc)
	if prepared != nil {
		plan = prepared.Plan
	}
	use, err := u.StartUse(ctx)
	if err != nil {
		return err
	}
	use.Location = loc
	if !use.Ready {
		fmt.Fprintln(&brief, "相关记忆（引用 ID 与版本；长期约束继续适用）：")
	}
	// Rank through the same scoped retrieval used by recall instead of
	// filling the prompt with globally recent memories. No nested model
	// request is made while the owner's transaction is locked.
	query := HistoryQuery(item.Title, history, c.Prompt)
	if err := u.Overflow(ctx, "retrieval_query_bytes", len(query)-len(contextbuilder.Tail(query, HistoryBytes))); err != nil {
		return err
	}
	query = contextbuilder.Tail(query, HistoryBytes)
	tokens := memory.SearchTokens(query)
	if len(tokens) > queryTerms {
		if err := u.Overflow(ctx, "retrieval_query_terms", len(tokens)-queryTerms); err != nil {
			return err
		}
		tokens = tokens[len(tokens)-queryTerms:]
	}
	request := memory.RecallRequest{Team: &memory.TeamRecall{Text: query, Plan: plan, ThingID: &item.ID, ProjectID: &projectID, RankFusion: use.Ready}, Query: query, Mode: memory.Remember, Context: memory.WorkingContext{Objects: []memory.ID{}}}
	if projectID != "" {
		request.Context.Objects = append(request.Context.Objects, memory.ID(projectID))
	}
	recall, err := u.Recall(ctx, agent.ID, request, recallBudget, tokens)
	if err != nil {
		return err
	}
	for _, overflow := range []struct {
		reason string
		count  int
	}{{"recall_budget", recall.Coverage.Omitted}, {"source_recall_budget", recall.Coverage.OmittedSources}} {
		if err := u.Overflow(ctx, overflow.reason, overflow.count); err != nil {
			return err
		}
	}
	if recall.TimeRelaxed {
		fmt.Fprintln(&brief, contextbuilder.RecallTimeRelaxed)
	}
	excerpts := recall.Excerpts
	var preparedRefs []memory.Ref
	historyRequests := []string{}
	if prepared != nil {
		excerpts = append(append([]memory.RecallExcerpt{}, prepared.Excerpts...), excerpts...)
		preparedRefs = prepared.Refs
	}
	for _, turn := range history {
		historyRequests = append(historyRequests, turn.RequestID)
		if !turn.Outdated {
			artifactRefs = append(artifactRefs, turn.Refs...)
		}
	}
	historyText := HistoryText(history)
	if err := u.Overflow(ctx, "delegate_history_bytes", len(historyText)-len(contextbuilder.Tail(historyText, HistoryBytes))); err != nil {
		return err
	}
	brief.WriteString(contextbuilder.Tail(historyText, HistoryBytes))
	ranked := OrderMemories(memories, recall.Structured, preparedRefs, recall.Memories)
	tier := u.Tier(ctx)
	if tier == "" {
		tier = "heavy"
	}
	run.MemoryTier = MemoryTier(tier, use.Ready)
	use.Tier = run.MemoryTier
	if use.Ready {
		ranked = RankMemories(memories, agent, recall.Structured, preparedRefs, recall.Memories)
	}
	if err := u.FinishUse(ctx, agent, item.ID, c.Prompt, ranked, &use); err != nil {
		return err
	}
	if use.ProjectHandover != nil {
		run.ProjectHandoverWrittenAt = use.ProjectHandover.WrittenAt
	}
	run.MemoryGroups = use.Groups
	memoryStart := brief.Len()
	if err := u.Overflow(ctx, "memory_char_budget", brief.Memories(use, agent, loc)); err != nil {
		return err
	}
	if use.Ready {
		run.MemoryContextRange = &[2]int{memoryStart, brief.Len()}
	}
	contextbuilder.OrderExcerpts(excerpts, plan)
	sources, err := u.Sources(ctx, agent.ID, item.ID, excerpts, historyRequests, sourceSegments, sourceCharacters)
	if err != nil {
		return err
	}
	if err := u.Overflow(ctx, "source_excerpt_char_budget", brief.Excerpts(sources, loc)); err != nil {
		return err
	}
	groups, gaps, err := u.Evidence(ctx, agent.ID, item.ID, brief.Claims, use.Ready)
	if err != nil {
		return err
	}
	if err := u.Overflow(ctx, "evidence_context_char_budget", brief.Evidence(groups, gaps, loc)); err != nil {
		return err
	}
	// Derived copies carry input dependencies even when their source memories
	// fall outside this run's text budget.
	brief.Derived(artifactRefs)
	brief.Request(c.Prompt, c.Kind, plan.Recall || recall.TimeRelaxed)
	run.ContextMemoryIDs = append(run.ContextMemoryIDs, brief.MemoryIDs...)
	run.ContextVersions = uniqueRefs(append(run.ContextVersions, brief.Versions...))
	run.Brief = brief.String()
	status := "queued"
	if agent.Channel == "manual" {
		run.Status, status = "waiting", "waiting"
	} else {
		if !u.Available(agent.ID) {
			return memory.ErrUnavailable
		}
		run.Cost = u.Estimate(agent.ID, CostInput(run.Kind, run.Brief))
		day, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return err
		}
		local := now.In(day)
		spent, err := u.SpentSince(ctx, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, day))
		if err != nil {
			return err
		}
		if spent+run.Cost > settings.DailyBudget {
			return workspace.ErrBudget
		}
	}
	return u.Insert(ctx, run, status, reviseTarget)
}
