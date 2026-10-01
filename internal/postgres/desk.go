package postgres

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const deskInstructions = assistantInstructions + `
你在导办台回答用户随口问的一句话。给你的记录是按字面检索出来的，很多可能与问题无关：只用真正回答了问题的记录，无关的一律忽略，不要硬凑。
记录里没有答案时直说没有记录。天气、新闻、行情等实时或公开信息，能联网就上网查，查到了注明来自网络；查不了就直说，不要编。常识问题可以简短回答，但要说明不是来自记录。
搜索词会离开这次对话：只写公开信息需要的关键词，绝不能把记录或事项里的人名、数字、私事放进搜索词。
回答简短直接，像当面回话，用纯文本，不要 Markdown，网址不要写进回答而是放进 links。只输出 JSON：{"answer":"回答","used":["记录ID"],"links":["网址"]}。used 只列回答里真正用到的记录 ID；links 只列回答用到的网页，最多 3 个；没有就给空数组。`

var answerDeskSchema = []byte(`{"type":"object","additionalProperties":false,"required":["answer","used","links"],"properties":{"answer":{"type":"string"},"used":{"type":"array","maxItems":256,"items":{"type":"string"}},"links":{"type":"array","maxItems":3,"items":{"type":"string"}}}}`)

func (s *Store) answerDeskTyped(ctx context.Context, scope memory.Scope, agentID, question string, suppliedHistory []workspace.DeskTurn) (workspace.DeskAnswer, error) {
	var out workspace.DeskAnswer
	question = strings.TrimSpace(question)
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if question == "" || len(question) > 4000 || len(suppliedHistory) > 6 {
		return out, memory.ErrInvalid
	}
	for _, turn := range suppliedHistory {
		if len(turn.Question) > 4000 || len(turn.Answer) > 8000 {
			return out, memory.ErrInvalid
		}
	}
	if s.models == nil || !s.models.Available(agentID) {
		return out, memory.ErrUnavailable
	}
	history := append([]workspace.DeskTurn{}, suppliedHistory...)
	var agent workspace.Agent
	var task memory.TrustedTaskContext
	var memories []workspace.Memory
	var tasks []workspace.Item
	var settings workspace.Settings
	indirect := []memory.TypedDependency{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		agent, err = queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), agentID)
		if err != nil {
			return err
		}
		if !agent.Enabled || agent.Channel == "manual" {
			return memory.ErrUnavailable
		}
		task, err = s.trustedTaskContextTx(ctx, tx, scope, agent.ID, "secretary", memory.HardScope{Kind: memory.UnscopedContextScope}, nil)
		if err != nil {
			return err
		}
		task.View.KnownAt, task.View.ValidAt = &task.Now, &task.Now
		modelScope := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID, Task: &task}
		for i := range history {
			history[i].Answer = "" // The browser's claimed answer is never a source.
			if history[i].ID == "" {
				continue
			}
			if !memory.ID(history[i].ID).Valid() {
				return memory.ErrInvalid
			}
			if err := tx.QueryRow(ctx, "SELECT question,answer FROM desk_turns WHERE owner_id=$1 AND id=$2 AND agent_id=$3", string(scope.OwnerID), history[i].ID, agent.ID).Scan(&history[i].Question, &history[i].Answer); err != nil {
				return memory.ErrNotFound
			}
			deps, err := s.deskTurnContextTx(ctx, tx, scope, history[i].ID, &task)
			if err != nil {
				history[i].Answer = ""
				continue
			}
			indirect = mergeRunDependencies(indirect, deps)
		}
		var refs []memory.Ref
		memories, tasks, settings, refs, err = s.deskContextTx(ctx, tx, modelScope, agent, false)
		if err != nil {
			return err
		}
		entries, cov, err := hydrateTypedContextTx(ctx, tx, scope, task, refs)
		if err != nil {
			return err
		}
		if !cov.Complete {
			return memory.ErrConflict
		}
		indirect = mergeRunDependencies(indirect, dependenciesForEntries(entries))
		return s.verifyTaskDeskActionsTx(ctx, tx, scope, task)
	})
	if err != nil {
		return out, err
	}
	earlier := ""
	for _, turn := range history {
		earlier += turn.Question + " "
	}
	query := tail(earlier+question, 4000)
	modelScope := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID, Task: &task}
	recall, err := s.Recall(withRecallEmbeddingQuery(ctx, question), modelScope, memory.RecallRequest{Query: query, Mode: task.View.Mode, Context: memory.WorkingContext{Objects: []memory.ID{}, KnownAt: task.View.KnownAt, ValidAt: task.View.ValidAt}, Budget: task.MemoryBudget})
	if err != nil {
		return out, err
	}
	visible := map[string]workspace.Memory{}
	for _, m := range memories {
		if oneOf(m.Kind, agent.MemoryKinds...) && (m.Epistemic != "inferred" || agent.IncludeInferred) {
			visible[m.ID] = m
		}
	}
	refs := []memory.Ref{}
	candidates := []memory.CandidateRecord{}
	for _, ref := range recall.Memories {
		candidate := memory.CandidateRecord{Ref: ref, Stage: "recall", Disposition: "candidate"}
		if ref.Kind == memory.ClaimKind {
			if _, ok := visible[string(ref.ID)]; !ok {
				candidate.Disposition, candidate.Reason = "rejected", "context_filtered"
				candidates = append(candidates, candidate)
				continue
			}
		}
		refs = append(refs, ref)
		withSpan := false
		for _, span := range recall.SourceSpans {
			if span.Source == ref {
				copySpan := span
				candidate.SourceSpan = &copySpan
				candidates = append(candidates, candidate)
				withSpan = true
			}
		}
		if !withSpan {
			candidate.SourceSpan = nil
			candidates = append(candidates, candidate)
		}
	}
	var entries []memory.EvidenceEntry
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var cov memory.Coverage
		var err error
		entries, cov, err = hydrateTypedContextTx(ctx, tx, scope, task, refs)
		if err != nil {
			return err
		}
		if !cov.Complete {
			return memory.ErrConflict
		}
		entries, err = applyRecallSpans(entries, recall.SourceSpans)
		if err != nil {
			return err
		}
		entries = boundRunEntries(entries, query, task.MemoryBudget)
		return nil
	})
	if err != nil {
		return out, err
	}
	for i := range candidates {
		candidates[i].Disposition, candidates[i].Reason = "rejected", "context_filtered"
		for _, entry := range entries {
			if runCandidateContainsEntry(candidates[i], entry) {
				candidates[i].Disposition, candidates[i].Reason = "selected", ""
				break
			}
		}
	}
	deps := mergeRunDependencies(dependenciesForEntries(entries), indirect)
	var prompt strings.Builder
	prompt.WriteString(deskNow(deskLocation(settings)))
	if settings.City != "" {
		fmt.Fprintf(&prompt, "用户所在城市：%s\n", settings.City)
	}
	if len(history) > 0 {
		prompt.WriteString("\n同一张卡片上之前的对话：\n")
		for _, turn := range history {
			fmt.Fprintf(&prompt, "问：%s\n", turn.Question)
			if turn.Answer != "" {
				fmt.Fprintf(&prompt, "答：%s\n", turn.Answer)
			} else if turn.ID != "" {
				prompt.WriteString("（先前回答的依据已变化，请按当前允许的资料重新回答。）\n")
			}
		}
	}
	fmt.Fprintf(&prompt, "\n问题：%s\n\n检索到的记录（引用 ID）：\n", question)
	sent := map[string]memory.EvidenceEntry{}
	for i := range entries {
		entry := &entries[i]
		expressed := "unknown"
		if entry.ExpressedAt != nil {
			expressed = entry.ExpressedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(&prompt, "[%s / kind=%s / version=%d role=%s expressed_at=%s historical=%t changed=%t]\n", entry.Ref.ID, entry.Ref.Kind, entry.Ref.Version, entry.Role, expressed, entry.Historical, entry.Changed)
		if m, ok := visible[string(entry.Ref.ID)]; ok && entry.Ref.Kind == memory.ClaimKind {
			fmt.Fprintf(&prompt, "epistemic=%s confirmation=%s acquisition=%s\n", m.Epistemic, m.Confirmation, m.Acquisition)
		}
		appendContextEvidence(&prompt, string(entry.Ref.ID), entry)
		if prior, ok := sent[string(entry.Ref.ID)]; ok {
			prior.Text += "\n" + entry.Text
			sent[string(entry.Ref.ID)] = prior
		} else {
			sent[string(entry.Ref.ID)] = *entry
		}
	}
	if len(entries) == 0 {
		prompt.WriteString("（没有）\n")
	}
	prompt.WriteString("\n未完成的事项：\n")
	for _, item := range tasks {
		fmt.Fprintf(&prompt, "- %s（%s；截止 %s）\n", item.Title, item.Status, item.Due)
	}
	if len(tasks) == 0 {
		prompt.WriteString("（没有）\n")
	}
	check := func(ctx context.Context, tx pgx.Tx) error {
		live, err := s.contextRecipientModelTx(ctx, tx, scope, agent.ID, task.Recipient.Role, nil, task.Recipient.Model)
		if task.Recipient.Model == "unknown" {
			current, e := s.trustedTaskContextTx(ctx, tx, scope, agent.ID, task.Recipient.Role, task.Scope, nil)
			live, err = current.Recipient, e
		}
		if err != nil || live != task.Recipient {
			return memory.ErrConflict
		}
		if err := verifyTypedContextTx(ctx, tx, scope, task, deps); err != nil {
			return contextFenceError(err)
		}
		for _, item := range tasks {
			current, err := getItem(ctx, tx, scope, item.ID)
			if err != nil {
				return memory.ErrConflict
			}
			current, _, err = s.sanitizeItemTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID, Task: &task}, agent.ID, current)
			if err != nil {
				return err
			}
			if current.Title != item.Title || current.Status != item.Status || current.Due != item.Due {
				return memory.ErrConflict
			}
		}
		for _, turn := range history {
			if turn.ID != "" && turn.Answer != "" {
				if _, err := s.deskTurnContextTx(ctx, tx, scope, turn.ID, &task); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return check(ctx, tx) }); err != nil {
		return out, err
	}
	p, _ := s.models.Get(agent.ID)
	if err := s.reserveModelCost(ctx, scope.OwnerID, p.Reserve(deskInstructions+prompt.String()), nil); err != nil {
		return out, err
	}
	out.ID = string(memory.NewID())
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	result, attempt, err := s.generateContext(workCtx, scope, out.ID, task, deps, entries, candidates, indirect, deskInstructions, prompt.String(), answerDeskSchema)
	cancel()
	if err != nil {
		return workspace.DeskAnswer{}, err
	}
	task.Recipient = attempt.Manifest.Recipient
	var reply struct {
		Answer string   `json:"answer"`
		Used   []string `json:"used"`
		Links  []string `json:"links"`
	}
	text := strings.TrimSpace(result.Text)
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```"))
	if strictJSON([]byte(text), &reply) != nil || strings.TrimSpace(reply.Answer) == "" {
		reply.Answer, reply.Used, reply.Links = strings.TrimSpace(result.Text), nil, nil
	}
	claimed := []memory.Ref{}
	for _, id := range reply.Used {
		ref := memory.Ref{ID: memory.ID(id)}
		if entry, ok := sent[id]; ok {
			ref = entry.Ref
		} else {
			for _, candidate := range candidates {
				if string(candidate.Ref.ID) == id {
					ref = candidate.Ref
					break
				}
			}
		}
		claimed = append(claimed, ref)
	}
	if err := s.recordContextUsed(ctx, scope, attempt.ID, claimed); err != nil {
		return workspace.DeskAnswer{}, err
	}
	out.Answer, out.Agent = strings.TrimSpace(reply.Answer), agent.Name
	out.Used, out.Searches, out.Links = []workspace.DeskSource{}, []string{}, []string{}
	if len(result.Searches) > 0 {
		out.Searches = result.Searches[:min(len(result.Searches), 5)]
	}
	for _, link := range reply.Links {
		u, err := url.Parse(link)
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(link) <= 500 && len(out.Links) < 3 {
			out.Links = append(out.Links, u.String())
		}
	}
	for _, id := range reply.Used {
		if entry, ok := sent[id]; ok {
			out.Used = append(out.Used, workspace.DeskSource{Ref: entry.Ref, Text: entry.Text})
			delete(sent, id)
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := check(ctx, tx); err != nil {
			return err
		}
		if err := s.verifyContextAttemptTx(ctx, tx, scope, attempt.ID, task, deps); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO desk_turns(owner_id,id,agent_id,question,answer,dependencies,context_task) VALUES($1,$2,$3,$4,$5,$6,$7)", string(scope.OwnerID), out.ID, agent.ID, question, out.Answer, asJSON(refsForDependencies(deps)), asJSON(task)); err != nil {
			return err
		}
		return persistContextArtifactDependenciesTx(ctx, tx, scope, "desk_turn", out.ID, 1, task, deps)
	})
	if err != nil {
		return workspace.DeskAnswer{}, err
	}
	return out, nil
}

// AnswerDesk has the chosen assistant answer one desk question from the
// memories it may see, the open tasks and, where the provider offers it, the web. It is synchronous and short, unlike
// runs, so it reserves budget up front and never retries.
func (s *Store) AnswerDesk(ctx context.Context, scope memory.Scope, agentID, question string, history []workspace.DeskTurn) (workspace.DeskAnswer, error) {
	return s.answerDeskTyped(ctx, scope, agentID, question, history)
}

// tail keeps the last n bytes of s without splitting a character.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// deskContextTx keeps the legacy answer and secretary on the same visibility
// and derived-artifact rules. Stable ordering is used by the secretary cache.
func (s *Store) deskContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, stable bool) ([]workspace.Memory, []workspace.Item, workspace.Settings, []memory.Ref, error) {
	if scope.Task == nil || scope.Task.OwnerID != scope.OwnerID || scope.Task.Recipient.PrincipalID != agent.ID {
		return nil, nil, workspace.Settings{}, nil, memory.ErrForbidden
	}
	modelScope := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID, Task: scope.Task}
	memories, err := s.memoriesTx(ctx, tx, modelScope, true)
	if err != nil {
		return nil, nil, workspace.Settings{}, nil, err
	}
	order := "due_at NULLS LAST, updated_at DESC"
	if stable {
		order = "created_at,id"
	}
	tasks, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') ORDER BY "+order+" LIMIT 40", string(scope.OwnerID))
	if err != nil {
		return nil, nil, workspace.Settings{}, nil, err
	}
	dependencies := []memory.Ref{}
	for i := range tasks {
		var refs []memory.Ref
		tasks[i], refs, err = s.sanitizeItemTx(ctx, tx, modelScope, agent.ID, tasks[i])
		if err != nil {
			return nil, nil, workspace.Settings{}, nil, err
		}
		dependencies = append(dependencies, refs...)
	}
	settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
	return memories, tasks, settings, dependencies, err
}
func deskLocation(settings workspace.Settings) *time.Location {
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}
func deskNow(loc *time.Location) string {
	now := time.Now().In(loc)
	return fmt.Sprintf("现在：%s 星期%s（%s）\n", now.Format("2006-01-02 15:04"), []string{"日", "一", "二", "三", "四", "五", "六"}[now.Weekday()], loc)
}
