package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var assistantInstructions = prompts.Must("assistant").Text()

// deputyInstructions are for work handed to an agent. Research and drafting
// often need what is public, so the agent may search where its channel can;
// what it sends out as a search must not carry the owner's private material.
var deputyInstructions = prompts.Must("deputy").Text()

func (s *Store) runCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	if c.Type == "requestRun" {
		if err := deputy.CheckRequest(c.Kind, c.Prompt); err != nil {
			return err
		}
		item, err := getItem(ctx, tx, scope, c.ThingID)
		if err != nil {
			return err
		}
		agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.AgentID)
		if err != nil {
			return err
		}
		if !agent.Enabled {
			return memory.ErrForbidden
		}
		targetHash := workspace.ContentHash(item)
		item, artifactRefs, err := sanitizeItemTx(ctx, tx, scope, agent.ID, item)
		if err != nil {
			return err
		}
		history, err := readRunHistoryTx(ctx, tx, scope, c, item)
		if err != nil {
			return err
		}
		id, err := uuidOrNew(c.ID)
		if err != nil {
			return err
		}
		memories, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryReadOptions{useCurrent: true})
		if err != nil {
			return err
		}

		run := workspace.Run{DocumentVersions: []workspace.RunDocumentVersion{}, DocumentID: c.DocumentID, BaseVersion: c.BaseVersion, TargetHash: targetHash, SmokeID: smokeID(ctx), ID: id, ThingID: item.ID, AgentID: agent.ID, Kind: c.Kind, Prompt: c.Prompt, Status: "running", ContextMemoryIDs: []string{}, ContextVersions: []memory.Ref{}, CreatedAt: stamp()}
		var brief deputy.Brief
		projectID := item.ProjectID
		var project *workspace.Item
		if item.Kind == "project" {
			projectID = item.ID
		} else if projectID != "" {
			found, err := getItem(ctx, tx, scope, projectID)
			if err != nil {
				return err
			}
			project = &found
		}
		brief.Item(item, project)
		docs, docRefs, err := currentRunDocsTx(ctx, tx, scope, item, agent.ID)
		if err != nil {
			return err
		}

		var reviseTarget int
		if c.Kind == "revise" {
			if !memory.ID(c.DocumentID).Valid() || c.BaseVersion < 1 {
				return memory.ErrInvalid
			}
			target, e := queryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.DocumentID)
			if e != nil {
				return e
			}
			if target.ThingID != item.ID {
				return memory.ErrInvalid
			}
			reviseTarget = target.Version
			base, e := documentVersionTx(ctx, tx, scope, c.DocumentID, c.BaseVersion)
			if e != nil {
				return e
			}
			brief.ReviseBase(c.DocumentID, c.BaseVersion, base.Title, base.Body)
		}
		reviseID := ""
		if c.Kind == "revise" {
			reviseID = c.DocumentID
		}
		omittedDocs := brief.CurrentDocuments(docs, reviseID)
		run.DocumentVersions = append(run.DocumentVersions, brief.Documents...)
		if omittedDocs > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", "document_char_budget", omittedDocs); err != nil {
				return err
			}
		}
		artifactRefs = append(artifactRefs, docRefs...)
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		loc := deskLocation(settings)
		plan := memory.PlanQuery(c.Prompt, s.businessNow(), loc)
		if prepared, ok := ctx.Value(runContextKey{}).(preparedRunContext); ok {
			plan = prepared.Plan
		}
		u, err := s.startUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		u.Location = loc
		if !u.Ready {
			fmt.Fprintln(&brief, "相关记忆（引用 ID 与版本；长期约束继续适用）：")
		}
		// Rank through the same scoped retrieval used by Recall instead of
		// filling the prompt with globally recent memories. No nested model
		// request is made while the owner's transaction is locked.
		query := runHistoryQuery(item.Title, history, c.Prompt)
		if omitted := len(query) - len(tail(query, delegateContextBytes)); omitted > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", "retrieval_query_bytes", omitted); err != nil {
				return err
			}
		}
		query = tail(query, delegateContextBytes)
		tokens := memory.SearchTokens(query)
		if len(tokens) > 120 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", "retrieval_query_terms", len(tokens)-120); err != nil {
				return err
			}
			tokens = tokens[len(tokens)-120:]
		}
		terms := []string{}
		for _, token := range tokens {
			if len([]rune(token)) > 1 {
				terms = append(terms, "'"+strings.ReplaceAll(token, "'", "''")+"'")
			}
		}
		recall := memory.RecallResult{Coverage: retrieval.NewCoverage()}
		request := memory.RecallRequest{Team: &memory.TeamRecall{Text: query, Plan: plan, ThingID: &item.ID, ProjectID: &projectID, RankFusion: u.Ready}, Query: query, Mode: memory.Remember, Context: memory.WorkingContext{Objects: []memory.ID{}}}
		if projectID != "" {
			request.Context.Objects = append(request.Context.Objects, memory.ID(projectID))
		}
		budget := memory.Budget{Candidates: 100, Tokens: 10000, Edges: 30, Hops: 1}
		if err := s.recallTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID, Team: true}, request, budget, query, strings.Join(terms, " | "), nil, "", 0, "", tokens, &recall); err != nil {
			return err
		}
		for reason, n := range map[string]int{"recall_budget": recall.Coverage.Omitted, "source_recall_budget": recall.Coverage.OmittedSources} {
			if n > 0 {
				if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", reason, n); err != nil {
					return err
				}
			}
		}
		if recall.TimeRelaxed {
			fmt.Fprintln(&brief, contextbuilder.RecallTimeRelaxed)
		}
		excerpts := recall.Excerpts
		var preparedRefs []memory.Ref
		historyRequests := []string{}
		if prepared, ok := ctx.Value(runContextKey{}).(preparedRunContext); ok {
			excerpts = append(append([]memory.RecallExcerpt{}, prepared.Excerpts...), excerpts...)
			preparedRefs = prepared.Refs
		}
		for _, turn := range history {
			historyRequests = append(historyRequests, turn.RequestID)
			if !turn.Outdated {
				artifactRefs = append(artifactRefs, turn.Refs...)
			}
		}
		historyText := runHistoryText(history)
		if omitted := len(historyText) - len(tail(historyText, delegateContextBytes)); omitted > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", "delegate_history_bytes", omitted); err != nil {
				return err
			}
		}
		brief.WriteString(tail(historyText, delegateContextBytes))
		ordered := deputy.OrderMemories(memories, recall.Structured, preparedRefs, recall.Memories)
		run.MemoryTier = memoryTierForStatus(memoryTier(ctx, c.Prompt, "heavy"), u.Ready)
		u.Tier = run.MemoryTier
		ranked := ordered
		if u.Ready {
			ranked = deputy.RankMemories(memories, agent, recall.Structured, preparedRefs, recall.Memories)
		}
		if err = s.finishUseContextTx(ctx, tx, scope, agent, &item.ID, c.Prompt, ranked, &u); err != nil {
			return err
		}
		if u.ProjectHandover != nil {
			run.ProjectHandoverWrittenAt = u.ProjectHandover.WrittenAt
		}
		run.MemoryGroups = u.Groups
		overflow := func(reason string, count int) error {
			if count == 0 {
				return nil
			}
			return stageEventTx(ctx, tx, scope.OwnerID, "deputy", "overflow", reason, count)
		}
		memoryStart := brief.Len()
		if err := overflow("memory_char_budget", brief.Memories(u, agent, loc)); err != nil {
			return err
		}
		if u.Ready {
			run.MemoryContextRange = &[2]int{memoryStart, brief.Len()}
		}
		contextbuilder.OrderExcerpts(excerpts, plan)
		sources, err := teamSourceExcerptsTx(ctx, tx, scope, agent.ID, &item.ID, excerpts, historyRequests, 8, 4000)
		if err != nil {
			return err
		}
		if err := overflow("source_excerpt_char_budget", brief.Excerpts(sources, loc)); err != nil {
			return err
		}
		evidenceCtx := ctx
		if u.Ready {
			evidenceCtx = context.WithValue(ctx, useEvidenceBatchKey{}, true)
		}
		groups, gaps, err := evidenceContextsTx(evidenceCtx, tx, scope, agent.ID, &item.ID, brief.Claims)
		if err != nil {
			return err
		}
		if err := overflow("evidence_context_char_budget", brief.Evidence(groups, gaps, loc)); err != nil {
			return err
		}
		// Derived copies carry input dependencies even when their source memories
		// fall outside this run's text budget.
		brief.Derived(artifactRefs)
		recallDates := ""
		if plan.Recall || recall.TimeRelaxed {
			recallDates = recallDateInstructions
		}
		brief.Request(c.Prompt, c.Kind, recallDates)
		run.ContextMemoryIDs = append(run.ContextMemoryIDs, brief.MemoryIDs...)
		run.ContextVersions = append(run.ContextVersions, brief.Versions...)
		run.ContextVersions = uniqueRefs(run.ContextVersions)
		run.Brief = brief.String()
		dbStatus := "queued"
		if agent.Channel == "manual" {
			run.Status = "waiting"
			dbStatus = "waiting"
		} else {
			if s.models == nil || !s.models.Available(agent.ID) {
				return memory.ErrUnavailable
			}
			p, _ := s.models.Get(agent.ID)
			run.Cost = p.Reserve(deputyInstructions + run.Brief)
			loc, err := time.LoadLocation(settings.Timezone)
			if err != nil {
				return err
			}
			now := time.Now().In(loc)
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			var spent float64
			if err := tx.QueryRow(ctx, "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1 AND created_at >= $2),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1 AND created_at >= $2),0)", string(scope.OwnerID), start).Scan(&spent); err != nil {
				return err
			}
			if spent+run.Cost > settings.DailyBudget {
				return workspace.ErrBudget
			}
		}
		if _, err := tx.Exec(ctx, "INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", string(scope.OwnerID), run.ID, run.ThingID, run.AgentID, dbStatus, run.Cost, run.CreatedAt, asJSON(run)); err != nil {
			return err
		}

		if c.Kind == "revise" {
			if _, err := tx.Exec(ctx, `INSERT INTO revise_run_targets(owner_id,run_id,document_id,target_version) VALUES($1,$2,$3,$4)`, string(scope.OwnerID), run.ID, c.DocumentID, reviseTarget); err != nil {
				return err
			}
		}
		for _, ref := range run.ContextVersions {
			if _, err := tx.Exec(ctx, "INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4)", string(scope.OwnerID), run.ID, string(ref.ID), ref.Version); err != nil {
				return err
			}
		}
		return nil
	}
	if !memory.ID(c.ID).Valid() {
		return memory.ErrInvalid
	}
	run, err := queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID)
	if err != nil {
		return err
	}
	switch c.Type {
	case "pasteRunResult":
		if err := deputy.CanPaste(run); err != nil {
			return err
		}
		if run.Kind == "revise" {
			reason, err := manualReviseReasonTx(ctx, tx, scope, run, c.Output)
			if err != nil {
				return err
			}
			if reason != "" {
				run.Error = reason
				if err := stageEventTx(ctx, tx, scope.OwnerID, "deputy", "failure", "revise_invalid_output", 1); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, asJSON(run))
				return err
			}
		}
		if err := deputy.Paste(&run, c.Output, time.Now()); err != nil {
			return err
		}
	case "discardRun":
		if err := deputy.Discard(&run, time.Now()); err != nil {
			return err
		}
	case "adoptRun":
		if err := s.adoptRunTx(ctx, tx, scope, &run, c.As, c.Text, c.RequestID, false); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE agent_runs SET status=$3,document=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, run.Status, asJSON(run))
	if err != nil {
		return err
	}
	if c.Type == "pasteRunResult" {
		return s.autoAdoptRunTx(ctx, tx, scope, &run)
	}
	return nil
}

func (s *Store) adoptRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run, as, text, actionID string, auto bool) error {
	if run.SmokeID != "" {
		ctx = withSmoke(ctx, run.SmokeID)
	}
	if err := deputy.CheckAdoption(*run, as, text); err != nil {
		return err
	}
	// A completed draft can be adopted again after undo, whose audit/version
	// changes do not alter its content. Generation fences the destination;
	// adoption still fences revoked or deleted input and disabled agents.
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	if err := verifyRunAccessTx(ctx, useClockTx{Tx: tx, at: time.Now()}, scope, *run); err != nil {
		return err
	}
	item, err := getItem(ctx, tx, scope, run.ThingID)
	if err != nil {
		return err
	}
	// Undo restores the run document but leaves provenance audit rows. Clear
	// the previous adoption before reusing this run, including progress keys.
	if _, err := tx.Exec(ctx, "DELETE FROM adopted_artifacts WHERE owner_id=$1 AND run_id=$2", string(scope.OwnerID), run.ID); err != nil {
		return err
	}
	switch as {
	case "doc":
		if run.Kind == "revise" {
			if err := s.adoptReviseDocumentTx(ctx, tx, scope, *run, text); err != nil {
				return err
			}
		} else if err := saveDoc(ctx, tx, scope, workspace.Doc{ID: string(memory.NewID()), ThingID: item.ID, Title: item.Title + " · " + run.Kind, Body: text, By: "ai", RunID: run.ID, CreatedAt: stamp(), UpdatedAt: stamp()}); err != nil {
			return err
		}
	case "progress":
		if item.Kind == "project" {
			if err := s.adoptProjectMemoryTx(ctx, tx, scope, run, item, text, actionID, auto); err != nil {
				return err
			}
		} else if item.Kind == "task" {
			item.Notes += "\n" + text
			if err := artifactTx(ctx, tx, scope, run.ID, item.ID, "notes", run.ID, text); err != nil {
				return err
			}
		} else {
			item.Body += "\n" + text
			if err := artifactTx(ctx, tx, scope, run.ID, item.ID, "body", run.ID, text); err != nil {
				return err
			}
		}
	case "subtasks":
		lines, err := deputy.Subtasks(text)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if item.Kind == "task" {
				check := workspace.Check{ID: string(memory.NewID()), Text: line}
				item.Checklist = append(item.Checklist, check)
				if err := artifactTx(ctx, tx, scope, run.ID, item.ID, "check", check.ID, line); err != nil {
					return err
				}
			} else {
				task := workspace.NewItem("task", line)
				task.History[0].By = actorFromContext(ctx)
				task.Evolution[0].By = actorFromContext(ctx)
				if item.Kind == "project" {
					task.ProjectID = item.ID
				} else {
					task.IdeaID = item.ID
					task.ProjectID = item.ProjectID
				}
				if err := saveItem(ctx, tx, scope, task); err != nil {
					return err
				}
				if err := artifactTx(ctx, tx, scope, run.ID, task.ID, "task", task.ID, line); err != nil {
					return err
				}
			}
		}
	}
	item.Version++
	item.UpdatedAt = stamp()
	if err := s.saveAction(ctx, tx, scope, item, "采纳副手结果"); err != nil {
		return err
	}
	run.Adopted = &workspace.Adoption{As: as, At: stamp(), Edited: !auto && text != run.Output, Auto: auto, ActionID: actionID}
	if err := sampleTx(ctx, tx, scope, workspace.Sample{ID: string(memory.NewID()), Kind: "adopted-result", Prompt: run.Brief, Response: text, Origin: workspace.Origin{Label: item.Title, RunID: run.ID}, Version: 1, State: "candidate", Epistemic: "inferred", CreatedAt: stamp()}); err != nil {
		return err
	}
	if !auto {
		for _, ref := range run.ContextVersions {
			if err := recordUseTx(ctx, tx, scope, memory.UseEvent{Ref: ref, EventID: run.ID + ":" + string(ref.ID), Kind: "adoption", At: time.Now()}); err != nil && !errors.Is(err, memory.ErrConflict) && !errors.Is(err, memory.ErrNotFound) {
				return err
			}
		}
	}
	return nil
}

func verifyRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	var item *workspace.Item
	if run.ThingID != "" {
		current, err := getItem(ctx, tx, scope, run.ThingID)
		if err != nil {
			return err
		}
		item = &current
	}
	return verifyRunForItemTx(ctx, tx, scope, run, item)
}

// Derived answers obey the destination item's current context policy too.
// Checking only the agent grant would let a previous output reintroduce an
// excluded memory, or a memory from the item's former project.
func verifyRunForItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run, item *workspace.Item) error {
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	var projectID string
	var excluded []string
	if item != nil {
		projectID = item.ProjectID
		if item.Kind == "project" {
			projectID = item.ID
		}
		excluded, err = queryDocuments[string](ctx, tx, "SELECT to_jsonb(memory_id::text) FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
		if err != nil {
			return err
		}
	}
	claimIDs, sourceIDs := []string{}, []string{}
	for _, ref := range uniqueRefs(run.ContextVersions) {
		if ref.Kind == memory.SourceKind {
			sourceIDs = append(sourceIDs, string(ref.ID))
		} else {
			claimIDs = append(claimIDs, string(ref.ID))
		}
	}
	statuses, err := claimStatusesTx(ctx, tx, scope.OwnerID, claimIDs)
	if err != nil {
		return err
	}
	type dependency struct {
		ID                  string
		Version             int
		Nature, Acquisition string
		Scope               map[string]json.RawMessage
	}
	claims := map[string]dependency{}
	if len(claimIDs) > 0 {
		// Resolve applicable versions once. Preserve the original readClaim grant,
		// revision, nature, inference, and project checks for every supplied identity.
		rows, err := queryDocuments[dependency](ctx, tx, `WITH applicable AS MATERIALIZED (
 SELECT claim_id,version FROM applicable_claim_versions($1,now(),now()) WHERE claim_id=ANY($2::uuid[])
) SELECT jsonb_build_object('id',r.id,'version',c.version,'nature',c.nature,'acquisition',c.acquisition,'scope',c.scope)
 FROM applicable a JOIN memory_records r ON r.owner_id=$1 AND r.id=a.claim_id
 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,a.version)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 WHERE r.state='active' AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$3)`, string(scope.OwnerID), claimIDs, run.AgentID)
		if err != nil {
			return err
		}
		for _, claim := range rows {
			claims[claim.ID] = claim
		}
	}
	sources := map[string]int{}
	if len(sourceIDs) > 0 {
		var thingID *string
		if item != nil && item.ID != "" {
			thingID = &item.ID
		}
		rows, err := queryDocuments[dependency](ctx, tx, `SELECT jsonb_build_object('id',r.id,'version',r.version)
 FROM memory_records r JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=$1 AND r.id=ANY($2::uuid[]) AND r.kind='source' AND r.state='active' AND v.state='active' AND `+teamSourceVisibleSQL("$1", "r.id", "$3", "$4"), string(scope.OwnerID), sourceIDs, run.AgentID, thingID)
		if err != nil {
			return err
		}
		for _, source := range rows {
			sources[source.ID] = source.Version
		}
	}
	for _, ref := range uniqueRefs(run.ContextVersions) {
		if ref.Kind == memory.SourceKind {
			version, ok := sources[string(ref.ID)]
			if !ok || version != ref.Version {
				return memory.ErrConflict
			}
			continue
		}
		if !retirementDependencyAllowed(ctx, statuses[string(ref.ID)]) {
			return memory.ErrConflict
		}
		claim, ok := claims[string(ref.ID)]
		if !ok || claim.Version != ref.Version || !oneOf(claim.Nature, agent.MemoryKinds...) || !agent.IncludeInferred && (claim.Acquisition == "inferred" || statuses[string(ref.ID)].AI) {
			return memory.ErrConflict
		}
		if item != nil {
			var claimProject string
			if raw, ok := claim.Scope["project_id"]; ok && modelcall.DecodeStrict(raw, &claimProject) != nil {
				return memory.ErrConflict
			}
			if oneOf(string(ref.ID), excluded...) || claimProject != "" && claimProject != projectID {
				return memory.ErrConflict
			}
		}
	}
	return nil
}

// A version change alone makes an answer outdated. Recheck every dependency
// against its current version before deciding whether old content is safe to
// retain; a removed grant or deleted dependency must dominate any correction.
func verifyRunAccessTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	ctx = context.WithValue(ctx, retirementAccessKey{}, true)
	run.ContextVersions = append([]memory.Ref(nil), run.ContextVersions...)
	ids := make([]string, len(run.ContextVersions))
	for i, ref := range run.ContextVersions {
		ids[i] = string(ref.ID)
	}
	rows, err := queryDocuments[struct {
		ID      string
		Version int
	}](ctx, tx, `SELECT jsonb_build_object('id',id,'version',version) FROM memory_records WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND state='active'`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	versions := map[string]int{}
	for _, row := range rows {
		versions[row.ID] = row.Version
	}
	for i, ref := range run.ContextVersions {
		version, ok := versions[string(ref.ID)]
		if !ok {
			return memory.ErrConflict
		}
		run.ContextVersions[i].Version = version
	}
	return verifyRunTx(ctx, tx, scope, run)
}

// uniqueRefs keeps the first occurrence of each exact reference. Dependency
// lists are sets: history and recall can name the same memory many times.
func uniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// RunAgentJobs owns costly jobs: an ambiguous provider timeout/crash is marked
// failed and is never automatically submitted again (which could double bill).
func (s *Store) RunAgentJobs(ctx context.Context, logger *slog.Logger, steps deputy.Steps) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if recovery, err := s.recoverInteractiveCallsOnce(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("interactive accounting recovery failed", "error_type", fmt.Sprintf("%T", err), "pending_results", recovery.PendingResults, "pending_accounting", recovery.PendingAccounting, "interrupted_calls", recovery.Interrupted, "counts_known", recovery.CountsKnown)
			}
			if err := s.runAgent(ctx, steps); err != nil && ctx.Err() == nil {
				logger.Warn("agent job failed", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}
}

// RunDeputy makes the single worker pass for bounded evaluation.
// Selection, leases, generation, and application remain in the normal workflow.
func (s *Store) RunDeputy(ctx context.Context, steps deputy.Steps) error {
	return s.runAgent(ctx, steps)
}

func (s *Store) runAgent(ctx context.Context, steps deputy.Steps) (resultErr error) {
	started := time.Now()
	ctx, timing := newExecutionTimer(ctx, "deputy", started)
	ctx, persistCancel := context.WithDeadline(ctx, started.Add(heavyUseTimeout))
	defer persistCancel()
	s.pendingPaid.Range(func(key, value any) bool {
		var snap reviseSnapshot
		paid, ok := value.(*paidModelResult)
		if !ok || json.Unmarshal(paid.Prompt, &snap) != nil || snap.Run.Kind != "revise" {
			return true
		}
		_, _ = s.pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_token=NULL,lease_until=NULL WHERE id=$1 AND document->>'createdAt'=$2 AND agent_id=$3 AND thing_id=$4 AND status='running' AND lease_until<clock_timestamp()`, string(key.(memory.ID)), snap.Run.CreatedAt, snap.Run.AgentID, snap.Run.ThingID)
		return true
	})
	// A durable returned result, or a recorded pre-submission failure, can resume.
	// Input-only started calls cannot authorize another paid submission.
	s.pendingInteractive.Range(func(_, value any) bool {
		entry := value.(*pendingInteractiveResult)
		policy := entry.request.Policy.(interactiveCallPolicy)
		if policy.BudgetOwner == "deputy_run" {
			_, _ = s.pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_token=NULL,lease_until=NULL WHERE owner_id=$1 AND id=$2 AND document->>'createdAt'=$3 AND status='running' AND lease_until<clock_timestamp()`, string(entry.request.OwnerID), string(entry.request.ExecutionID), policy.Origin)
		}
		return true
	})
	recoverableSQL := `EXISTS(SELECT 1 FROM background_model_results b WHERE b.owner_id=agent_runs.owner_id AND b.job_id=agent_runs.id AND b.purpose='revise') OR EXISTS(SELECT 1 FROM model_calls c WHERE c.owner_id=agent_runs.owner_id AND c.execution_id=agent_runs.id AND c.stage='answer' AND c.input_manifest->>'budgetOwner'='deputy_run' AND c.input_manifest->>'origin'=agent_runs.document->>'createdAt' AND ((c.result_receipt->'billing' IS NOT NULL AND c.result_receipt->'billing'<>'null'::jsonb) OR (c.outcome='failed' AND c.accounting_state IN ('not_reserved','settled'))))`
	_, err := s.pool.Exec(ctx, `UPDATE agent_runs SET status='failed',lease_until=NULL,lease_token=NULL,document=document||jsonb_build_object('status','failed','error','进程中断，结果和用量未确认；未自动重试','finishedAt',now()) WHERE status='running' AND lease_until<now() AND NOT (`+recoverableSQL+`)`)
	if err != nil {
		return err
	}
	if err := (interactiveCalls{store: s}).recordInterruptedDeputyApplications(ctx); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_until=NULL,lease_token=NULL WHERE status='running' AND lease_until<now() AND (`+recoverableSQL+`)`); err != nil {
		return err
	}
	var cached *paidModelResult
	var run workspace.Run
	var scope memory.Scope
	var token string
	var savedRow *interactiveCallRow
	var savedInput *reviseSnapshot
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var ownerID, id string
		var data []byte
		err := tx.QueryRow(ctx, `SELECT owner_id::text,id::text,document FROM agent_runs WHERE status='queued' AND ($1::boolean OR (`+recoverableSQL+`)) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, s.models != nil).Scan(&ownerID, &id, &data)
		if err != nil {
			return err
		}
		scope = memory.Scope{OwnerID: memory.ID(ownerID), PrincipalID: "owner", IsOwner: true}
		if err := modelcall.DecodeStrict(data, &run); err != nil {
			return err
		}
		if !oneOf(run.MemoryTier, "light", "medium", "heavy") {
			run.MemoryTier = "heavy"
		}
		if run.Kind == "revise" {
			cached, err = s.paidModelResult(ctx, worker.Job{ID: memory.ID(run.ID), OwnerID: scope.OwnerID})
			if err != nil {
				return err
			}
			if cached != nil {
				var original reviseSnapshot
				if json.Unmarshal(cached.Prompt, &original) != nil || original.Run.ID != run.ID || original.Run.CreatedAt != run.CreatedAt || original.Run.AgentID != run.AgentID || original.Run.ThingID != run.ThingID {
					cached = nil
				}
			}
		}
		savedRow, savedInput, err = (interactiveCalls{store: s}).deputyInput(ctx, tx, scope.OwnerID, run)
		if err != nil {
			return err
		}
		if cached == nil && savedRow == nil {
			if err := checkQueuedUseRunPromptTx(ctx, tx, scope, run); err != nil {
				run.Status = "failed"
				run.Cost = 0
				run.Error = "记忆或授权已变化，请重新生成"
				_, err = tx.Exec(ctx, "UPDATE agent_runs SET status='failed',reserved_cost=0,document=$3 WHERE owner_id=$1 AND id=$2", ownerID, id, asJSON(run))
				return err
			}
		}
		if run.Kind == "revise" && cached == nil && savedRow == nil {
			if err := validateReviseTargetTx(ctx, tx, scope, run); err != nil {
				run.Status = "failed"
				run.Error = "目标文档已改变或不存在，请重新发起修改"
				_, e := tx.Exec(ctx, "UPDATE agent_runs SET status='failed',document=$3,reserved_cost=0 WHERE owner_id=$1 AND id=$2", ownerID, id, asJSON(run))
				return e
			}
		}
		token = string(memory.NewID())
		_, err = tx.Exec(ctx, "UPDATE agent_runs SET status='running',lease_token=$3,lease_until=now()+interval '5 minutes' WHERE owner_id=$1 AND id=$2", ownerID, id, token)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || token == "" {
		return err
	}
	timing.id = run.ID
	timing.Tier = run.MemoryTier
	defer s.finishExecutionTiming(ctx, scope.OwnerID, timing)
	if cached != nil && run.Kind == "revise" {
		return s.finishPaidRevise(ctx, scope, run, token, cached)
	}
	// A write failure makes only the saved execution available for the next pass.
	defer func() {
		if resultErr == nil {
			return
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		defer cancel()
		_, _ = s.pool.Exec(persist, `UPDATE agent_runs SET lease_until=clock_timestamp(),document=document||jsonb_build_object('error','处理未完成；已保存的模型结果将复用，不会自动重复生成') WHERE owner_id=$1 AND id=$2 AND document->>'createdAt'=$3 AND lease_token=$4 AND status='running'`, string(scope.OwnerID), run.ID, run.CreatedAt, token)
	}()
	execution := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: memory.ID(run.ID), RootExecutionID: memory.ID(run.ID), Function: "deputy", Policy: interactiveCallPolicy{Kind: "deputy", Token: token, Origin: run.CreatedAt, AgentID: run.AgentID, ThingID: run.ThingID}}
	ctx = modelcall.WithExecution(ctx, execution)
	workCtx, cancel := context.WithDeadline(ctx, started.Add(heavyUseTimeout-10*time.Second))
	defer cancel()
	if s.models != nil && s.models.ReloadSubscription && s.models.Codex != nil {
		defer s.models.Codex.Close()
	}
	var u useContext
	var useErr error
	if savedInput != nil {
		run = savedInput.Run
		u.Ready = savedInput.MemoryReady
		u.Coverage = savedInput.Coverage
	}
	if savedRow == nil {
		timing.beginPrepare()
		prepared, agent, prepareErr := s.deputyUseContext(workCtx, scope, &run)
		u, useErr = prepared, prepareErr
		if useErr == nil {
			run.MemoryTier = memoryTierForStatus(run.MemoryTier, u.Ready)
		}
		if useErr == nil && u.Ready {
			run.ProjectHandoverWrittenAt = nil
			if u.ProjectHandover != nil {
				run.ProjectHandoverWrittenAt = u.ProjectHandover.WrittenAt
			}
			if run.MemoryTier == "heavy" {
				taskText := deputy.HeavyTask(run)
				readerCtx, readerCancel := context.WithDeadline(workCtx, started.Add(heavyReaderBudget))
				picked, refs, keys := steps.Reader.Read(readerCtx, ctx, reader.Task{Scope: scope, Agent: agent, Thing: &run.ThingID, Text: taskText, Run: run.ID, Phase: "reader:prepare"}, u)
				readerCancel()
				run.MemoryGroups = keys
				run.ContextVersions = uniqueRefs(append(run.ContextVersions, refs...))
				u.Cards = nil
				u.Supplemental = picked
			}
			run.ContextVersions = uniqueRefs(append(run.ContextVersions, u.Dependencies...))
			deputy.RefreshMemory(&run, u)
		}
	}
	verifyErr := useErr
	if verifyErr == nil && savedRow == nil {
		verifyErr = pgx.BeginFunc(workCtx, s.pool, func(tx pgx.Tx) error { return checkUseRunPromptTx(workCtx, tx, scope, run) })
	}
	result := ai.Result{}
	timing.finishPrepare()
	answerStarted := time.Now()
	generationErr := verifyErr
	var mainPaid *modelcall.PaidResult
	var mainInvocation memory.ID
	request := deputyAnswerRequest(scope.OwnerID, token, reviseSnapshot{Run: run, RawPrompt: run.Brief, Coverage: u.Coverage, MemoryReady: u.Ready})
	if savedRow != nil && savedInput == nil {
		mainInvocation = savedRow.ID
		generationErr = &modelcall.Failure{Code: savedRow.ErrorCode, InvocationID: savedRow.ID}
		if savedRow.ErrorCode == "" {
			generationErr = modelcall.ErrNotApplicable
		}
	} else if verifyErr == nil {
		answerCtx, answerCancel := context.WithTimeout(workCtx, 60*time.Second)
		mainPaid, generationErr = steps.Calls.Answer(answerCtx, request)
		answerCancel()
		var persistence *modelcall.PersistenceError
		if errors.As(generationErr, &persistence) {
			return generationErr
		}
		if mainPaid != nil {
			mainInvocation = mainPaid.InvocationID
			result = ai.Result{Text: mainPaid.Output, Searches: mainPaid.Searches, Cost: mainPaid.Cost}
		}
		row, err := latestInteractiveCall(ctx, s.pool, request)
		if err != nil {
			return err
		}
		if row != nil {
			savedRow = row
			mainInvocation = row.ID
		}
	}
	if run.Kind == "revise" && generationErr == nil && mainPaid != nil {
		return s.finishPaidRevise(ctx, scope, run, token, mainPaid)
	}
	cost := result.Cost
	if savedRow != nil {
		var billed *float64
		if savedRow.Receipt.Billing != nil {
			billed = &savedRow.Receipt.Billing.Cost
		}
		cost = deputy.SettledCost(result.Cost, billed, savedRow.Outcome == "unknown", savedRow.Mode.Reserved)
	}
	// Only an unused admission without a linked reservation is settled here.
	// The gateway already owns every linked reservation and usage record.
	if savedRow == nil || savedRow.Reservation == "" {
		if err := s.settleRunCost(ctx, scope.OwnerID, run, cost); err != nil {
			return err
		}
	}
	selfcheckFallback := ""
	var selection *deputyResultSelection
	var reviewInvocation memory.ID
	if generationErr == nil && mainPaid != nil {
		var selected string
		selection, selected, err = (interactiveCalls{store: s}).deputySelection(ctx, scope.OwnerID, run, mainInvocation)
		if err != nil {
			return err
		}
		if selection != nil {
			result.Text = selected
			selfcheckFallback = selection.Fallback
			reviewInvocation = selection.Review
		}
	}
	// A prior review without a durable decision is incomplete. Keep the draft.
	// Never repeat its paid call or reinterpret a late response as a new decision.
	reviewAlreadyAttempted := false
	if selection == nil && generationErr == nil && deputy.Selfchecks(run.MemoryTier) {
		readErr := s.pool.QueryRow(ctx, `SELECT id::text FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='selfcheck' AND input_manifest->>'origin'=$3 ORDER BY created_at DESC,id DESC LIMIT 1`, string(scope.OwnerID), run.ID, run.CreatedAt).Scan(&reviewInvocation)
		if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
			return readErr
		}
		reviewAlreadyAttempted = readErr == nil
		if reviewAlreadyAttempted {
			selfcheckFallback = "selfcheck_interrupted"
		}
	}
	if selection == nil && !reviewAlreadyAttempted && generationErr == nil && deputy.Selfchecks(run.MemoryTier) {
		checkBudget := deputy.SelfcheckBudget(time.Since(answerStarted))
		checkCtx, checkCancel := context.WithTimeout(workCtx, checkBudget)
		checkedText := ""
		err := s.checkDeputySelfcheckContext(checkCtx, scope, run, token)
		if err == nil {
			request := deputy.SelfcheckRequest(scope.OwnerID, token, run, result.Text)
			var paid *modelcall.PaidResult
			paid, err = steps.Calls.Selfcheck(checkCtx, request)
			readCtx, readCancel := context.WithTimeout(context.WithoutCancel(checkCtx), modelcall.PersistenceTimeout)
			row, readErr := latestInteractiveCall(readCtx, s.pool, request)
			readCancel()
			if readErr != nil {
				checkCancel()
				return readErr
			}
			if row != nil {
				reviewInvocation = row.ID
			}
			if err == nil {
				checkedText = paid.Output
				err = checkCtx.Err()
			}
		}
		checkCancel()
		var fallback string
		if result.Text, fallback = deputy.AcceptSelfcheck(result.Text, checkedText, err); fallback != "" {
			selfcheckFallback = fallback
			slog.WarnContext(ctx, "memory selfcheck fallback", "stage", "selfcheck", "error_type", secretary.ErrorType("selfcheck", err))
		}
	}
	if generationErr == nil && mainPaid != nil {
		if selection == nil {
			selected := mainInvocation
			if selfcheckFallback == "" && reviewInvocation != "" {
				selected = reviewInvocation
			}
			selection = &deputyResultSelection{Main: mainInvocation, Output: selected, OutputHash: fmt.Sprintf("%x", sha256.Sum256([]byte(result.Text))), Review: reviewInvocation, Fallback: selfcheckFallback}
		}
		if err := (interactiveCalls{store: s}).saveDeputySelection(ctx, request, *selection); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	timing.beginWrite(run.MemoryTier)
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		current, err := queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND lease_until>now() AND document->>'createdAt'=$4 FOR UPDATE", string(scope.OwnerID), run.ID, token, run.CreatedAt)
		if errors.Is(err, memory.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, ref := range run.ContextVersions {
			if _, err := tx.Exec(ctx, "INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", string(scope.OwnerID), run.ID, string(ref.ID), ref.Version); err != nil {
				return err
			}
			if !oneOf(string(ref.ID), run.ContextMemoryIDs...) {
				run.ContextMemoryIDs = append(run.ContextMemoryIDs, string(ref.ID))
			}
		}
		current.ContextMemoryIDs = run.ContextMemoryIDs
		current.ContextVersions = run.ContextVersions
		current.ProjectHandoverWrittenAt = run.ProjectHandoverWrittenAt
		current.MemoryGroups = run.MemoryGroups
		current.MemoryTier = run.MemoryTier
		current.MemoryContextRange = run.MemoryContextRange
		current.Brief = run.Brief
		current.FinishedAt = stamp()
		current.Status = "done"
		current.Output = result.Text
		current.Searches = result.Searches
		if generationErr != nil {
			current.Status = "failed"
			current.Error = "模型调用未完成，结果和用量可能未确认；请检查登录、额度与服务配置"
			if verifyErr != nil {
				current.Error = "生成前记忆或授权已变化，请重新生成"
			}
			var capability *ai.CapabilityError
			if errors.As(generationErr, &capability) {
				current.Error = "模型通道不支持必需能力：" + capability.Capability
			}
			var provider *siwc.ProviderError
			if errors.As(generationErr, &provider) {
				current.Error = provider.Message()
				current.ProviderError = asJSON(provider)
			}
		}
		current.Cost = cost
		if checkUseRunPromptTx(ctx, tx, scope, current) != nil {
			current.StaleContext = true
		}
		if verifyRunAccessTx(ctx, tx, scope, current) != nil {
			current.StaleContext = true
			current.Status = "failed"
			current.Output = ""
			current.Error = "生成期间授权已撤回或资料已删除，请重新生成"
		}
		if current.Output != "" && u.Coverage != nil && len(u.Coverage.Skipped) > 0 {
			current.Output += "\n这几组没来得及看：" + strings.Join(u.Coverage.Skipped, "、")
		}

		if _, err := tx.Exec(ctx, "UPDATE agent_runs SET status=$4,reserved_cost=$5,document=$6,lease_until=NULL,lease_token=NULL WHERE owner_id=$1 AND id=$2 AND lease_token=$3", string(scope.OwnerID), run.ID, token, current.Status, current.Cost, asJSON(current)); err != nil {
			return err
		}
		if err := s.autoAdoptRunTx(ctx, tx, scope, &current); err != nil {
			return err
		}
		// Work handed off is reported back where reminders go, so the person who
		// asked from their phone hears how it ended without opening the page.
		noticeReason := deputy.NoticeReason(current)
		if current.Status == "done" && u.Coverage != nil && len(u.Coverage.Notices) > 0 {
			noticeReason += "\n" + strings.Join(u.Coverage.Notices, "\n")
		}
		application, why := "applied", ""
		if current.Status != "done" || current.StaleContext {
			application, why = "not_applicable", "run_failed_or_stale"
		}
		if generationErr != nil && savedRow != nil && savedRow.ErrorCode != "" {
			why = savedRow.ErrorCode
		}
		if err := (interactiveCalls{store: s}).recordDeputyApplicationTx(ctx, tx, scope.OwnerID, current, mainInvocation, application, why); err != nil {
			return err
		}
		if generationErr == nil && run.MemoryTier != "light" {
			application := "applied"
			if selfcheckFallback != "" {
				application = "skipped"
			}
			if current.Status != "done" {
				application = "not_applicable"
			}
			if err := (interactiveCalls{store: s}).recordDeputyApplicationTx(ctx, tx, scope.OwnerID, current, reviewInvocation, application, selfcheckFallback); err != nil {
				return err
			}
		}
		if selfcheckFallback != "" {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "selfcheck", "failure", selfcheckFallback, 1); err != nil {
				return err
			}
			if current.Status == "done" {
				if selfcheckFallback == "selfcheck_output_rejected" {
					noticeReason += "\n自查结果未采用，保留了原稿。"
				} else {
					noticeReason += "\n自查未完成，保留了原稿。"
				}
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,now(),$4) ON CONFLICT DO NOTHING`,
			string(scope.OwnerID), current.ThingID, runNoticePrefix+current.ID, noticeReason); err != nil {
			return err
		}
		// Delivery triggers expose background results without changing the
		// revision used to authorize checkbox, reminder and bulk commands.
		return nil
	})
}

func (s *Store) autoAdoptRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run) error {
	if run.Status != "done" || run.StaleContext || run.Adopted != nil || deputy.TrimChecklist(run.Output) == "" {
		return nil
	}
	// Completion and billing are already saved in the outer transaction. Only
	// adoption writes may be rolled back, including SQL errors that abort a tx.
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer savepoint.Rollback(ctx)
	// adoptRunTx sets Adopted before its final writes. Keep that mutation out of
	// the caller's completed run until the savepoint has successfully committed.
	adopted := *run
	if err := s.autoAdoptResultTx(ctx, savepoint, scope, &adopted); err != nil {
		rollbackErr := savepoint.Rollback(ctx)
		// SQL/provider error messages can contain private text. Log only IDs and
		// the error's type, never the output, prompt, or raw error message.
		slog.WarnContext(ctx, "assistant result auto-adoption failed", "run_id", run.ID, "error_type", fmt.Sprintf("%T", err))
		// A failed rollback leaves the outer transaction unusable. Ordinary
		// adoption failures return nil so completion can still commit.
		return rollbackErr
	}
	if err := savepoint.Commit(ctx); err != nil {
		return err
	}
	*run = adopted
	return nil
}

func (s *Store) autoAdoptResultTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run) error {
	if run.SmokeID != "" {
		ctx = withSmoke(ctx, run.SmokeID)
	}
	item, err := getItem(ctx, tx, scope, run.ThingID)
	if err != nil {
		return err
	}
	as := deputy.AdoptionFor(*run, run.Output)
	summary := "副手结果：存成文档"
	if as == "progress" {
		summary = "副手结果：写进进度"
		if item.Kind == "project" {
			summary = "副手结果：记住项目进展"
		}
	}
	if as == "subtasks" {
		summary = fmt.Sprintf("副手结果：加了 %d 个子任务", len(deputy.Checklist(run.Output)))
	}
	id := string(memory.NewID())
	ctx = withActor(ctx, "assistant")
	ctx = withActionLog(ctx, id, "worker", "", summary)
	if err := beginActionLogTx(ctx, tx); err != nil {
		return err
	}
	if err := s.adoptRunTx(ctx, tx, scope, run, as, run.Output, id, true); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, asJSON(run)); err != nil {
		return err
	}
	return flushActionLog(ctx, tx, scope)
}

// runNoticePrefix marks a notice that reports how handed-off work ended,
// as opposed to a reminder that came due.
const runNoticePrefix = "run:"
