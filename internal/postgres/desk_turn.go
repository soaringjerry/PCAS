package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var secretaryInstructions = prompts.Must("secretary").Text()

// Persist only the exchange. Workspace state is always read at response time.
type storedSecretaryResponse struct {
	ConversationID    string                  `json:"conversationId"`
	AttachmentContext string                  `json:"attachmentContext,omitempty"`
	Turn              workspace.SecretaryTurn `json:"turn"`
}

type secretaryContext struct {
	Documents         map[string]workspace.Doc
	TargetItems       map[string]workspace.Item
	ConversationID    string
	Agent             workspace.Agent
	Settings          workspace.Settings
	Aliases           map[string]workspace.Item
	Items             []workspace.Item
	Memories          map[string]workspace.Memory
	Sources           map[string]workspace.DeskSourceItem
	Dependencies      []memory.Ref
	History           []workspace.SecretaryTurn
	Projects          []workspace.Item
	Tasks             []workspace.Item
	Ideas             []workspace.Item
	Recent            []workspace.Item
	Counts            map[string]int
	AttachmentContext string
	Plan              memory.QueryPlan
	Use               useContext
	Tier              string
}

func stringPointer(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func pointerValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Store) secretaryContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, req workspace.DeskTurnRequest, conversationID string) (secretaryContext, error) {
	out := secretaryContext{TargetItems: map[string]workspace.Item{}, ConversationID: conversationID, Aliases: map[string]workspace.Item{}, Memories: map[string]workspace.Memory{}, Counts: map[string]int{}}
	agentID := req.AgentID
	if agentID == "" {
		agentID = s.models.ExtractionID()
	}
	if s.models == nil {
		return out, memory.ErrUnavailable
	}
	var err error
	out.Agent, err = queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), agentID)
	if errors.Is(err, memory.ErrNotFound) {
		return out, memory.ErrUnavailable
	}
	if err != nil {
		return out, err
	}
	if !out.Agent.Enabled || out.Agent.Channel == "manual" || !s.models.Available(out.Agent.ID) {
		return out, memory.ErrUnavailable
	}
	tasks, settings, deps, err := s.deskItemsContextTx(ctx, tx, scope, out.Agent, true)
	if err != nil {
		return out, err
	}
	out.Settings = settings
	out.Tasks = tasks
	out.Dependencies = deps
	out.Projects, err = queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='project' AND status='active' ORDER BY created_at,id LIMIT 50", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	goals, err := projectGoalEvidenceTx(ctx, tx, scope.OwnerID)
	if err != nil {
		return out, err
	}
	for i := range out.Projects {
		p := &out.Projects[i]
		excerpt := ""
		if p.Creation != nil && p.Creation.Source != nil {
			excerpt = p.Creation.Source.Excerpt
		}
		p.Goal = projectMeaning(p.Goal, excerpt, goals[p.ID])
	}
	out.Ideas, err = queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM (SELECT document,created_at,id FROM work_items WHERE owner_id=$1 AND kind='idea' ORDER BY created_at DESC,id DESC LIMIT 20) recent ORDER BY created_at,id", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Recent, err = queryDocuments[workspace.Item](ctx, tx, `SELECT w.document FROM work_items w JOIN (
 SELECT c->>'id' AS id,max(l.created_at) AS at FROM action_log l JOIN desk_turns t ON (t.owner_id,t.id)=(l.owner_id,l.turn_id)
 CROSS JOIN LATERAL jsonb_array_elements(l.changes) c
 WHERE l.owner_id=$1 AND t.conversation_id=$2 AND l.undone_at IS NULL AND c->>'table'='work_items' GROUP BY c->>'id'
 ) r ON r.id=w.id::text WHERE w.owner_id=$1 ORDER BY r.at DESC,w.created_at,w.id LIMIT 20`, string(scope.OwnerID), conversationID)
	if err != nil {
		return out, err
	}
	for _, group := range []struct {
		prefix string
		items  *[]workspace.Item
	}{{"P", &out.Projects}, {"T", &out.Tasks}, {"I", &out.Ideas}, {"R", &out.Recent}} {
		for i := range *group.items {
			item := (*group.items)[i]
			original, err := getItem(ctx, tx, scope, item.ID)
			if err != nil {
				return out, err
			}
			out.TargetItems[item.ID] = original
			var refs []memory.Ref
			item, refs, err = sanitizeItemTx(ctx, tx, scope, out.Agent.ID, item)
			if err != nil {
				return out, err
			}
			(*group.items)[i] = item
			out.Aliases[fmt.Sprintf("%s%d", group.prefix, i+1)] = item
			out.Items = append(out.Items, item)
			out.Dependencies = append(out.Dependencies, refs...)
		}
	}
	if req.ThingID != nil {
		item, err := getItem(ctx, tx, scope, *req.ThingID)
		if err != nil {
			return out, err
		}
		out.TargetItems[item.ID] = item
		item, refs, err := sanitizeItemTx(ctx, tx, scope, out.Agent.ID, item)
		if err != nil {
			return out, err
		}
		out.Aliases["THIS"] = item
		out.Items = append(out.Items, item)
		out.Dependencies = append(out.Dependencies, refs...)
	}
	rows, err := tx.Query(ctx, "SELECT project_id::text,count(*) FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') AND project_id IS NOT NULL GROUP BY project_id", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var count int
		if err = rows.Scan(&id, &count); err != nil {
			rows.Close()
			return out, err
		}
		out.Counts[id] = count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	stored, err := s.deskTurnsTx(ctx, tx, scope, conversationID, 6, out.Agent.ID, req.ThingID, &out.Dependencies)
	if err != nil {
		return out, err
	}
	if err := recordSecretaryOverflowTx(ctx, tx, scope, out); err != nil {
		return out, err
	}
	out.History = stored.Turns
	return out, nil
}
func (s *Store) checkDeskContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent string, dependencies []memory.Ref, items []workspace.Item, full bool) error {
	if err := verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: agent, ContextVersions: dependencies}); err != nil {
		return err
	}
	for _, sent := range items {
		current, err := getItem(ctx, tx, scope, sent.ID)
		if err != nil {
			return memory.ErrConflict
		}
		current, _, err = sanitizeItemTx(ctx, tx, scope, agent, current)
		if err != nil {
			return err
		}
		if full {
			if !bytes.Equal(asJSON(current), asJSON(sent)) {
				return memory.ErrConflict
			}
		} else if current.Title != sent.Title || current.Status != sent.Status || current.Due != sent.Due {
			return memory.ErrConflict
		}
	}
	return nil
}
func (s *Store) secretaryPrompt(ctx context.Context, tx pgx.Tx, scope memory.Scope, req workspace.DeskTurnRequest, c *secretaryContext, search secretary.Recall) (string, map[string]workspace.Memory, error) {
	var prompt strings.Builder
	loc := deskLocation(c.Settings)
	c.Plan = memory.PlanQuery(req.Text, s.businessNow(), loc)
	var projectID *string
	if req.ThingID != nil {
		item, err := getItem(ctx, tx, scope, *req.ThingID)
		if err != nil {
			return "", nil, err
		}
		project := item.ProjectID
		if item.Kind == "project" {
			project = item.ID
		}
		projectID = &project
	}
	docCatalog, docErr := s.secretaryDocumentsTx(ctx, tx, scope, req, c)
	if docErr != nil {
		return "", nil, docErr
	}
	prompt.WriteString(docCatalog)
	view := secretary.WorkspaceView{City: c.Settings.City, Location: loc, Projects: c.Projects, OpenCounts: c.Counts, Tasks: c.Tasks, Ideas: c.Ideas, Recent: c.Recent}
	earlier := ""
	for _, t := range c.History {
		turn := secretary.HistoryTurn{Text: t.Text, Reply: t.Reply, Outdated: t.Outdated}
		earlier += t.Text + " "
		for _, receipt := range t.Receipts {
			line := secretary.HistoryReceipt{Text: receipt.Text, Undone: receipt.Undone}
			// A receipt of an item that the agent may not read now is hidden.
			if t.Text != "" && receipt.ThingID != nil {
				if item, err := getItem(ctx, tx, scope, *receipt.ThingID); err == nil {
					permitted, _, err := sanitizeItemTx(ctx, tx, scope, c.Agent.ID, item)
					if err != nil {
						return "", nil, err
					}
					line.Hidden = permitted.Title != item.Title
				} else {
					// An item that no longer exists gives no line.
					continue
				}
			}
			turn.Receipts = append(turn.Receipts, line)
		}
		view.History = append(view.History, turn)
	}
	workspaceText, clipped := secretary.RenderWorkspace(view)
	for _, characters := range clipped {
		if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "project_evidence_characters", characters); err != nil {
			return "", nil, err
		}
	}
	prompt.WriteString(workspaceText)
	c.Tier = memoryTier(ctx, req.Text, "light")
	var err error
	c.Use, err = s.startUseContextTx(ctx, tx, scope)
	c.Tier = memoryTierForStatus(c.Tier, c.Use.Ready)
	c.Use.Location = loc
	c.Use.Tier = c.Tier
	if err != nil {
		return "", nil, err
	}
	tokens := 4000
	candidates := 15
	if c.Use.Ready {
		candidates = 40
		tokens = 12000
	}
	recall, err := search(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.Agent.ID, Team: true}, memory.RecallRequest{Team: &memory.TeamRecall{Text: req.Text, Plan: c.Plan, ThingID: req.ThingID, ProjectID: projectID, Candidates: 20, RankFusion: c.Use.Ready}, Query: tail(earlier+req.Text, 4000), Mode: "remember", Context: memory.WorkingContext{Objects: []memory.ID{}}, Budget: memory.Budget{Candidates: candidates, Tokens: tokens, Edges: 15, Hops: 1}})
	if err != nil {
		return "", nil, err
	}
	for reason, n := range map[string]int{"recall_budget": recall.Coverage.Omitted, "source_recall_budget": recall.Coverage.OmittedSources} {
		if n > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", reason, n); err != nil {
				return "", nil, err
			}
		}
	}
	ids := make([]string, 0, len(recall.Memories))
	for _, ref := range recall.Memories {
		ids = append(ids, string(ref.ID))
	}
	memories, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: c.Agent.ID}, true, memoryReadOptions{ids: ids, useCurrent: true})
	if err != nil {
		return "", nil, err
	}
	for _, m := range memories {
		if contextbuilder.MemoryAllowed(m, c.Agent) {
			c.Memories[m.ID] = m
		}
	}
	ranked := []workspace.Memory{}
	for _, ref := range recall.Memories {
		if m, ok := c.Memories[string(ref.ID)]; ok {
			ranked = append(ranked, m)
		}
	}
	if err = s.finishUseContextTx(ctx, tx, scope, c.Agent, req.ThingID, req.Text, ranked, &c.Use); err != nil {
		return "", nil, err
	}
	given := secretary.Memories{Use: c.Use, Location: loc, Recalled: ranked, TimeRelaxed: recall.TimeRelaxed}
	if req.ThingID != nil {
		given.Readable = func(m workspace.Memory) bool {
			ref := memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
			return verifyRunTx(ctx, tx, scope, workspace.Run{ThingID: *req.ThingID, AgentID: c.Agent.ID, ContextVersions: []memory.Ref{ref}}) == nil
		}
	}
	shown := secretary.RenderMemories(given)
	prompt.WriteString(shown.Text)
	sent, contextClaims := shown.Sent, shown.Claims
	c.Dependencies = append(c.Dependencies, shown.Dependencies...)
	historyRequests, err := queryDocuments[string](ctx, tx, "SELECT to_jsonb(request_id::text) FROM desk_turns WHERE owner_id=$1 AND conversation_id=$2 AND question!='' AND request_id IS NOT NULL", string(scope.OwnerID), c.ConversationID)
	if err != nil {
		return "", nil, err
	}
	contextbuilder.OrderExcerpts(recall.Excerpts, c.Plan)
	excerpts, err := teamSourceExcerptsTx(ctx, tx, scope, c.Agent.ID, req.ThingID, recall.Excerpts, historyRequests, 6, 2400)
	if err != nil {
		return "", nil, err
	}
	c.Sources = map[string]workspace.DeskSourceItem{}
	excerptText, excerptRefs := secretary.RenderExcerpts(excerpts, loc, c.Sources)
	prompt.WriteString(excerptText)
	c.Dependencies = append(c.Dependencies, excerptRefs...)
	evidenceCtx := ctx
	if c.Use.Ready {
		evidenceCtx = context.WithValue(ctx, useEvidenceBatchKey{}, true)
	}
	groups, gaps, err := evidenceContextsTx(evidenceCtx, tx, scope, c.Agent.ID, req.ThingID, contextClaims)
	if err != nil {
		return "", nil, err
	}
	evidenceText, evidenceRefs := secretary.RenderEvidence(groups, gaps, loc, c.Sources)
	prompt.WriteString(evidenceText)
	c.Dependencies = append(c.Dependencies, evidenceRefs...)
	// Each of the last turns already carries its own history. Without this the
	// stored list doubles every turn of a conversation.
	if err := s.secretaryHandoverEvidenceTx(ctx, tx, scope, c, &prompt, sent); err != nil {
		return "", nil, err
	}
	c.Dependencies = uniqueRefs(c.Dependencies)
	request := secretary.Request{Now: s.businessNow(), Location: loc, Text: req.Text, Attachments: c.AttachmentContext}
	if t, ok := c.Aliases["THIS"]; ok {
		request.This = &t
	}
	prompt.WriteString(secretary.RenderRequest(request))
	return secretary.HideIdentifiers(prompt.String()), sent, nil
}

// DeskTurn holds request and conversation advisory locks across generation.
// Owner row locks are acquired only for the short budget/commit transactions.
// Contended callers release their connection before waiting to retry.
// DeskStore gives the secretary service the storage of its turns.
func (s *Store) DeskStore() secretary.TurnStore { return deskStore{s} }

type deskStore struct{ store *Store }

func (a deskStore) Turn(ctx context.Context, scope memory.Scope, req workspace.DeskTurnRequest, steps secretary.Steps) (workspace.DeskTurnResponse, error) {
	s := a.store
	var out workspace.DeskTurnResponse
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	req, err := secretary.CheckRequest(req)
	if err != nil {
		return out, err
	}
	if req.SmokeID != "" {
		ctx = withSmoke(ctx, req.SmokeID)
	}
	text := strings.TrimSpace(req.Text)
	// Finish accepted input durably even when the caller disconnects. Model
	// generation still observes the caller cancellation below.
	requestCtx := ctx
	turnStarted := time.Now()
	requestCtx, timing := newExecutionTimer(requestCtx, "secretary", turnStarted)
	ctx = requestCtx
	defer s.finishExecutionTiming(ctx, scope.OwnerID, timing)
	// The answer model may request heavy reading after its first response.
	persistTimeout := heavyUseTimeout
	if memoryTier(ctx, req.Text, "light") == "heavy" {
		persistTimeout = heavyUseTimeout
		var generationCancel context.CancelFunc
		requestCtx, generationCancel = context.WithDeadline(requestCtx, turnStarted.Add(heavyUseTimeout-10*time.Second))
		defer generationCancel()
	}
	ctx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer persistCancel()
	hash := sha256.Sum256(asJSON(req)) // The original body, before trimming, fences retries.
	ownerPrepStarted := time.Now()
	prepared, err := s.prepareUseOwner(ctx, scope)
	if err != nil {
		return out, err
	}
	if !prepared {
		if _, err := s.Snapshot(ctx, scope); err != nil {
			return out, err
		}
	}
	timing.addPrepare(time.Since(ownerPrepStarted))
	if req.SmokeID != "" {
		if err := s.registerSmokeRequest(ctx, scope, req); err != nil {
			return out, err
		}
	}
	conversationID := pointerValue(req.ConversationID)
	if conversationID == "" {
		conversationID = string(memory.NewID())
	}
	ticket, err := s.admitSecretaryTurn(ctx, requestCtx, string(scope.OwnerID), req.RequestID, conversationID, hash[:])
	if err != nil {
		return out, err
	}
	conversationID = ticket.conversation
	execution := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: memory.ID(req.RequestID), RootExecutionID: memory.ID(req.RequestID), Function: "secretary", Policy: interactiveCallPolicy{Kind: "secretary", Token: ticket.creator, Origin: fmt.Sprintf("%x", hash), RequestHash: hash[:], ThingID: pointerValue(req.ThingID)}}
	requestCtx = modelcall.WithExecution(requestCtx, execution)
	ctx = modelcall.WithExecution(ctx, execution)
	var attachments []deskAttachment
	if !ticket.legacy && len(req.Attachments) > 0 {
		attachmentStarted := time.Now()
		attachments, err = s.prepareDeskAttachments(requestCtx, scope, req, steps.ReadAttachment)
		timing.addPrepare(time.Since(attachmentStarted))
		if err != nil {
			return out, err
		}
	}
	err = s.withOrderedSecretaryTurn(ctx, requestCtx, string(scope.OwnerID), req.RequestID, ticket, func(tx pgx.Tx, captureOnly bool) error {
		var priorHash, prior []byte
		var refs []memory.Ref
		var priorAgent string
		var erased bool
		err := tx.QueryRow(ctx, "SELECT request_hash,response,dependencies,agent_id,question='' AND answer='' FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&priorHash, &prior, &refs, &priorAgent, &erased)
		if err == nil {
			if !bytes.Equal(hash[:], priorHash) {
				return memory.ErrConflict
			}
			var saved storedSecretaryResponse
			if err := json.Unmarshal(prior, &saved); err != nil {
				return err
			}
			out.ConversationID, out.Turn = saved.ConversationID, saved.Turn
			// Deleted request replays retain the already scrubbed response.
			if !erased {
				refreshDeskTurnTx(ctx, tx, scope, &out.Turn, workspace.Run{AgentID: priorAgent, ContextVersions: refs}, false, false)
			}
			out.State, err = s.snapshotTx(ctx, tx, scope)
			if err != nil {
				return err
			}
			return refreshDeskReceiptUndoTx(ctx, tx, scope, []workspace.SecretaryTurn{out.Turn})
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out.ConversationID = conversationID
		out.Turn = workspace.SecretaryTurn{ID: string(memory.NewID()), Text: req.Text, Cards: []workspace.DeskCard{}, Receipts: []workspace.DeskReceipt{}, CreatedAt: stamp()}
		timing.id = out.Turn.ID
		if req.ThingID != nil && !captureOnly {
			if _, err := getItem(ctx, tx, scope, *req.ThingID); err != nil {
				return err
			}
		}
		session := &secretarySession{
			secretaryAsk:  &secretaryAsk{store: s, tx: tx, scope: scope, req: req, turnID: out.Turn.ID, requestCtx: requestCtx, timing: timing},
			secretaryTurn: &secretaryTurn{store: s, tx: tx, scope: scope, req: req, turnID: out.Turn.ID},
			conversation:  conversationID, attachments: attachments, text: text, started: turnStarted,
		}
		conducted, err := steps.Conduct(ctx, secretary.Unit{Session: session, Ask: session.secretaryAsk, Request: requestCtx, TurnID: out.Turn.ID, Thing: req.ThingID,
			Conducting: secretary.Conducting{Unordered: captureOnly, HasText: text != "", CheckOnly: req.SmokeID != ""}})
		if err != nil {
			return err
		}
		c := *session.c
		out.Turn.Agent, out.Turn.Reply, out.Turn.Ask = conducted.Agent, conducted.Reply, conducted.Ask
		out.Turn.Receipts = append(out.Turn.Receipts, conducted.Receipts...)
		dependencies := []memory.Ref{}
		if conducted.Finished {
			out.Turn.Cards = conducted.Cards
			dependencies = c.Dependencies
		}
		for _, a := range attachments {
			receipt, err := s.deskAttachmentReceiptTx(ctx, tx, scope, out.Turn.ID, a)
			if err != nil {
				return err
			}
			out.Turn.Receipts = append(out.Turn.Receipts, receipt)
			if a.Warning != "" {
				if out.Turn.Reply != "" {
					out.Turn.Reply += "\n"
				}
				out.Turn.Reply += a.Warning
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		out.State, err = s.snapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO desk_turns(owner_id,id,agent_id,question,answer,dependencies,conversation_id,thing_id,request_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", string(scope.OwnerID), out.Turn.ID, c.Agent.ID, req.Text, out.Turn.Reply, asJSON(dependencies), conversationID, pointerValueOrNull(req.ThingID), req.RequestID, hash[:], asJSON(storedSecretaryResponse{ConversationID: out.ConversationID, Turn: out.Turn, AttachmentContext: c.AttachmentContext}), out.Turn.CreatedAt)
		return err
	})
	if req.SmokeID != "" && errors.Is(err, pgx.ErrNoRows) {
		err = memory.ErrConflict // Cleanup may retire a registered, still-pending ticket.
	}
	return out, err
}

func secretaryReply(reply string) string { return strings.TrimSpace(reply) }

func pointerValueOrNull(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// secretarySession is one turn in its place in the conversation. It holds
// the context of the turn and gives the turn its reads and its writes in the
// transaction of the turn.
type secretarySession struct {
	*secretaryAsk
	*secretaryTurn
	c            *secretaryContext
	conversation string
	attachments  []deskAttachment
	text         string
	started      time.Time
	input        string
}

func (t *secretarySession) Context(ctx context.Context) (secretary.View, error) {
	s := t.secretaryAsk
	s.timing.beginPrepare()
	c, err := s.store.secretaryContextTx(ctx, s.tx, s.scope, s.req, t.conversation)
	s.timing.Tier = c.Tier
	for _, a := range t.attachments {
		if a.Context != "" {
			c.AttachmentContext += a.Context + "\n"
			c.Dependencies = append(c.Dependencies, a.Ref)
		}
	}
	t.c, t.secretaryAsk.c, t.secretaryTurn.c = &c, &c, &c
	t.secretaryTurn.sent = map[string]workspace.Memory{}
	view := secretary.View{Agent: c.Agent.Name, Aliases: c.Aliases}
	if len(c.History) > 0 {
		view.Last = &c.History[len(c.History)-1]
	}
	return view, err
}

func (t *secretarySession) Input(ctx context.Context, recall secretary.Recall) (*secretary.Asking, string, error) {
	s, c := t.secretaryAsk, t.c
	prompt, sent, err := s.store.secretaryPrompt(ctx, s.tx, s.scope, s.req, c, recall)
	if err != nil {
		return nil, "context", err
	}
	t.secretaryTurn.sent = sent
	if err := s.store.checkSecretaryUseContextTx(ctx, s.tx, s.scope, *c); err != nil {
		return nil, "verify", err
	}
	state := &secretary.Asking{Tier: c.Tier, Use: c.Use, Sent: sent, Input: prompt, Dependencies: c.Dependencies, Text: s.req.Text, Location: deskLocation(c.Settings), Started: t.started}
	if item, ok := c.Aliases["THIS"]; ok {
		state.This = &item
	}
	return state, "verify", nil
}

func (t *secretarySession) Asked(state *secretary.Asking) { t.secretaryAsk.sync(state) }

func (t *secretarySession) BeginWrite(ctx context.Context) error {
	s := t.secretaryAsk
	s.timing.beginWrite(t.c.Tier)
	_, err := s.tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(s.scope.OwnerID))
	return err
}

func (t *secretarySession) VerifyTargets(ctx context.Context, answer secretaryOutput) error {
	s := t.secretaryAsk
	return s.store.checkSecretaryActionTargetsTx(ctx, s.tx, s.scope, *t.c, answer)
}

func (t *secretarySession) CheckFailed(ctx context.Context, reason string) error {
	s := t.secretaryAsk
	return stageEventTx(ctx, s.tx, s.scope.OwnerID, "selfcheck", "failure", reason, 1)
}

func (t *secretarySession) RecordApplication(ctx context.Context, stage, application, reason string) error {
	s := t.secretaryAsk
	return (interactiveCalls{store: s.store}).recordApplicationTx(ctx, s.tx, s.scope.OwnerID, memory.ID(s.req.RequestID), stage, application, reason)
}

// A check-only turn and a turn without text keep nothing.
func (t *secretarySession) KeepSentence(ctx context.Context, unordered bool) error {
	s := t.secretaryAsk
	if t.text == "" || s.req.SmokeID != "" {
		return nil
	}
	if unordered {
		return s.store.captureIncompleteSecretaryTurn(ctx, s.tx, s.scope, s.req.RequestID, s.req.Text)
	}
	return s.store.commandTx(ctx, s.tx, s.scope, workspace.Command{Type: "capture", RequestID: s.req.RequestID, Text: s.req.Text})
}

func (t *secretarySession) SaveSentence(ctx context.Context) error {
	s := t.secretaryAsk
	original := workspace.SourceRef{Label: "秘书原话", Excerpt: s.req.Text, At: stamp()}
	if t.text != "" && s.req.SmokeID == "" {
		ref, err := s.store.ingestTx(ctx, s.tx, s.scope, memory.IngestRequest{Connector: "desk", ExternalID: s.req.RequestID, ExternalVersion: "1", Title: "秘书原话", Text: s.req.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		original.SourceID = string(ref.ID)
		original.Version = ref.Version
	}
	t.secretaryTurn.original = original
	return nil
}

// secretaryAsk is what the asking of one turn reads and records. The
// secretary service makes the model calls.
type secretaryAsk struct {
	store      *Store
	tx         pgx.Tx
	scope      memory.Scope
	req        workspace.DeskTurnRequest
	c          *secretaryContext
	turnID     string
	requestCtx context.Context
	timing     *executionTimer
}

// sync gives the turn's context what the asking changed. The checks of a
// call read the context.
func (m *secretaryAsk) sync(state *secretary.Asking) {
	m.c.Tier, m.c.Use, m.c.Dependencies = state.Tier, state.Use, state.Dependencies
}

func (m *secretaryAsk) Agent() workspace.Agent { return m.c.Agent }

func (m *secretaryAsk) Verify(ctx context.Context, state *secretary.Asking) error {
	m.sync(state)
	return m.store.checkSecretaryUseContextTx(ctx, m.tx, m.scope, *m.c)
}

func (m *secretaryAsk) VerifyCurrent(ctx context.Context, state *secretary.Asking) error {
	m.sync(state)
	c := *m.c
	return pgx.BeginTxFunc(ctx, m.store.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return m.store.checkSecretaryUseContextTx(ctx, tx, m.scope, c)
	})
}

func (m *secretaryAsk) Prepared()               { m.timing.finishPrepare() }
func (m *secretaryAsk) Reading(d time.Duration) { m.timing.addPrepare(d) }

func (m *secretaryAsk) SetTier(ctx context.Context, tier string) error {
	_, err := m.tx.Exec(ctx, "UPDATE model_usage SET tier=$3 WHERE owner_id=$1 AND turn_id=$2 AND purpose='secretary'", string(m.scope.OwnerID), m.turnID, tier)
	return err
}

// secretaryTurn is the unit of work in which the answer of one turn is
// applied. Its writes are in the transaction of the turn.
type secretaryTurn struct {
	store    *Store
	tx       pgx.Tx
	scope    memory.Scope
	req      workspace.DeskTurnRequest
	c        *secretaryContext
	turnID   string
	original workspace.SourceRef
	sent     map[string]workspace.Memory
}

// A memory that was deleted or changed since the turn showed it has no use
// to record.
func (t *secretaryTurn) RecordUse(ctx context.Context, m workspace.Memory, kind string) error {
	label := map[string]string{"adoption": ":adopt:", "user_mention": ":mention:"}[kind]
	err := recordContextUseTx(ctx, t.tx, t.scope, memory.UseEvent{Ref: memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}, EventID: t.turnID + label + m.ID, Kind: kind, At: time.Now()})
	if errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrNotFound) {
		return nil
	}
	return err
}

func (t *secretaryTurn) Overflow(ctx context.Context, reason string, count int) error {
	return stageEventTx(ctx, t.tx, t.scope.OwnerID, "secretary", "overflow", reason, count)
}

func (t *secretaryTurn) Item(ctx context.Context, id string) (workspace.Item, error) {
	return getItem(ctx, t.tx, t.scope, id)
}

func (t *secretaryTurn) Cards(ctx context.Context, answer secretaryOutput, sent map[string]workspace.Memory) ([]workspace.DeskCard, error) {
	return t.store.secretaryCardsTx(ctx, t.tx, t.scope, answer, sent, t.c.Sources, t.c.Aliases, deskLocation(t.c.Settings), t.c.Plan.Recall)
}

// Act does one action in a transaction of its own inside the turn, with its
// own action record, so that the owner can undo it alone.
func (t *secretaryTurn) Act(ctx context.Context, a secretaryAction, aliases map[string]workspace.Item) (secretary.ActionOutcome, error) {
	s, c := t.store, t.c
	out := secretary.ActionOutcome{ActionID: string(memory.NewID())}
	actionCtx := withActionLog(withActor(WithMemoryTier(ctx, c.Tier), "secretary"), out.ActionID, "desk", t.turnID, "秘书："+a.Op)
	projectReceipts := []workspace.DeskReceipt{}
	actionCtx = context.WithValue(actionCtx, secretaryCreationKey{}, t.original)
	actionCtx = context.WithValue(actionCtx, secretaryProjectReceiptsKey{}, &projectReceipts)
	actionCtx = context.WithValue(actionCtx, secretaryDocumentsKey{}, c.Documents)
	actionCtx = context.WithValue(actionCtx, secretaryMemoriesKey{}, t.sent)
	history := delegateHistoryContext{ConversationID: c.ConversationID}
	for _, turn := range c.History {
		history.IDs = append(history.IDs, turn.ID)
	}
	actionCtx = context.WithValue(actionCtx, delegateHistoryKey{}, history)
	actionTx, err := t.tx.Begin(ctx)
	if err != nil {
		return out, err
	}
	if err = beginActionLogTx(actionCtx, actionTx); err != nil {
		_ = actionTx.Rollback(ctx)
		return out, err
	}
	out.Receipt, out.Err = s.executeSecretaryActionTx(actionCtx, actionTx, t.scope, a, aliases, c.Agent, deskLocation(c.Settings), pointerValue(t.req.ThingID))
	if !secretary.Done(out.Receipt, out.Err) {
		return out, actionTx.Rollback(ctx)
	}
	// The summary and receipt describe the actual persisted result.
	actionCtx = withActionLog(actionCtx, out.ActionID, "desk", t.turnID, out.Receipt.Text)
	if err = flushActionLog(actionCtx, actionTx, t.scope); err != nil {
		_ = actionTx.Rollback(ctx)
		return out, err
	}
	if err = actionTx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM action_log WHERE owner_id=$1 AND id=$2)", string(t.scope.OwnerID), out.ActionID).Scan(&out.Changed); err != nil {
		_ = actionTx.Rollback(ctx)
		return out, err
	}
	out.Companions = projectReceipts
	if err = actionTx.Commit(ctx); err != nil {
		return out, err
	}
	out.Committed = true
	return out, nil
}

func (s *Store) DeskTurns(ctx context.Context, scope memory.Scope, conversationID string) (workspace.DeskTurnsResponse, error) {
	out := workspace.DeskTurnsResponse{ConversationID: conversationID, Turns: []workspace.SecretaryTurn{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(conversationID).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, err = s.deskTurnsTx(ctx, tx, scope, conversationID, 50, "", nil, nil)
		return err
	})
	return out, err
}
func (s *Store) deskTurnsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, conversationID string, limit int, agentOverride string, thingID *string, dependencies *[]memory.Ref) (workspace.DeskTurnsResponse, error) {
	out := workspace.DeskTurnsResponse{ConversationID: conversationID, Turns: []workspace.SecretaryTurn{}}
	type storedTurn struct {
		Response     storedSecretaryResponse `json:"response"`
		Dependencies []memory.Ref            `json:"dependencies"`
		AgentID      string                  `json:"agentId"`
		Erased       bool                    `json:"erased"`
		// The exchange's own original was deleted, as opposed to a memory it used.
		OriginalDeleted bool `json:"originalDeleted"`
	}
	turns, err := queryDocuments[storedTurn](ctx, tx, `SELECT jsonb_build_object('response',response,'dependencies',dependencies,'agentId',agent_id,'erased',question='' AND answer='','originalDeleted',original_deleted) FROM
 (SELECT response,dependencies,agent_id,question,answer,created_at,id,
   request_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM sources s WHERE s.owner_id=t.owner_id AND s.connector IN ('desk','capture','desk-incomplete') AND lower(s.external_id)=t.request_id::text) AS original_deleted
  FROM desk_turns t WHERE owner_id=$1 AND conversation_id=$2 AND response IS NOT NULL ORDER BY created_at DESC,id DESC LIMIT $3) recent ORDER BY created_at,id`, string(scope.OwnerID), conversationID, limit)
	if err != nil {
		return out, err
	}
	for _, stored := range turns {
		turn := stored.Response.Turn
		agent := stored.AgentID
		if agentOverride != "" {
			agent = agentOverride
		}
		run := workspace.Run{AgentID: agent, ContextVersions: stored.Dependencies}
		if thingID != nil {
			run.ThingID = *thingID
		}
		refreshDeskTurnTx(ctx, tx, scope, &turn, run, stored.Erased, stored.OriginalDeleted)
		if !stored.Erased && !turn.Outdated && dependencies != nil {
			*dependencies = append(*dependencies, stored.Dependencies...)
		}
		out.Turns = append(out.Turns, turn)
	}
	if err := refreshDeskReceiptUndoTx(ctx, tx, scope, out.Turns); err != nil {
		return out, err
	}
	return out, nil
}

const outdatedDeskAnswer = secretary.OutdatedAnswer

// Erasure keeps its existing placeholders; changed dependencies preserve the
// owner's exchange and are replaced only when assembling model history.
func refreshDeskTurnTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turn *workspace.SecretaryTurn, run workspace.Run, erased, originalDeleted bool) {
	turn.Outdated = false
	if erased {
		turn.Reply = "（这条回答依据的记忆已变更）"
		if originalDeleted {
			turn.Reply = "（内容已删除）"
		}
		turn.Cards = []workspace.DeskCard{}
		return
	}
	turn.Outdated = len(run.ContextVersions) > 0 && verifyRunTx(ctx, tx, scope, run) != nil
}

// Receipts retain their original action metadata in storage. Undo is a live
// workspace property, resolved in one owner-scoped query for the whole read.
func refreshDeskReceiptUndoTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turns []workspace.SecretaryTurn) error {
	ids := []string{}
	seen := map[string]bool{}
	for _, turn := range turns {
		for i := range turn.Receipts {
			receipt := &turn.Receipts[i]
			receipt.Undone = false
			if receipt.ActionID != nil && !seen[*receipt.ActionID] {
				seen[*receipt.ActionID] = true
				ids = append(ids, *receipt.ActionID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	undone, err := queryDocuments[string](ctx, tx, "SELECT to_jsonb(id::text) FROM action_log WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND undone_at IS NOT NULL", string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	byID := map[string]bool{}
	for _, id := range undone {
		byID[id] = true
	}
	for _, turn := range turns {
		for i := range turn.Receipts {
			receipt := &turn.Receipts[i]
			if receipt.ActionID != nil {
				receipt.Undone = byID[*receipt.ActionID]
			}
		}
	}
	return nil
}

func recordSecretaryOverflowTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretaryContext) error {
	rows, err := tx.Query(ctx, `SELECT kind,count(*) FROM work_items WHERE owner_id=$1 AND ((kind='task' AND status IN ('todo','doing','waiting')) OR (kind='project' AND status='active') OR kind='idea') GROUP BY kind`, string(scope.OwnerID))
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			rows.Close()
			return err
		}
		counts[kind] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for kind, limit := range map[string]int{"task": 40, "project": 50, "idea": 20} {
		if n := counts[kind] - limit; n > 0 {
			if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", kind+"_context_limit", n); err != nil {
				return err
			}
		}
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT w.id) FROM action_log l JOIN desk_turns t ON(t.owner_id,t.id)=(l.owner_id,l.turn_id) CROSS JOIN LATERAL jsonb_array_elements(l.changes) c JOIN work_items w ON w.owner_id=l.owner_id AND w.id::text=c->>'id' WHERE l.owner_id=$1 AND t.conversation_id=$2 AND l.undone_at IS NULL AND c->>'table'='work_items'`, string(scope.OwnerID), c.ConversationID).Scan(&recent); err != nil {
		return err
	}
	if recent > 20 {
		if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "recent_item_context_limit", recent-20); err != nil {
			return err
		}
	}
	var turns int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM desk_turns WHERE owner_id=$1 AND conversation_id=$2", string(scope.OwnerID), c.ConversationID).Scan(&turns); err != nil {
		return err
	}
	if turns > 6 {
		return stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "history_context_limit", turns-6)
	}
	return nil
}
