package postgres

import (
	"context"
	"errors"
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

// AnswerDesk has the chosen assistant answer one desk question from the
// memories it may see, the open tasks and, where the provider offers it, the web. It is synchronous and short, unlike
// runs, so it reserves budget up front and never retries.
func (s *Store) AnswerDesk(ctx context.Context, scope memory.Scope, agentID, question string, history []workspace.DeskTurn) (workspace.DeskAnswer, error) {
	var out workspace.DeskAnswer
	question = strings.TrimSpace(question)
	if !scope.IsOwner {
		return out, memory.ErrForbidden
	}
	if question == "" || len(question) > 4000 || len(history) > 6 {
		return out, memory.ErrInvalid
	}
	for _, turn := range history {
		if len(turn.Question) > 4000 || len(turn.Answer) > 8000 {
			return out, memory.ErrInvalid
		}
	}
	if s.models == nil || !s.models.Available(agentID) {
		return out, memory.ErrUnavailable
	}
	var agent workspace.Agent
	var memories []workspace.Memory
	var tasks []workspace.Item
	dependencies := []memory.Ref{}
	var settings workspace.Settings
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if agent, err = queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), agentID); err != nil {
			return err
		}
		if !agent.Enabled || agent.Channel == "manual" {
			return memory.ErrUnavailable
		}
		for i := range history {
			// IDs identify server-owned turns; legacy clients keep questions only.
			history[i].Answer = ""
			if history[i].ID == "" {
				continue
			}
			if !memory.ID(history[i].ID).Valid() {
				return memory.ErrInvalid
			}
			var refs []memory.Ref
			var question, answer string
			if err := tx.QueryRow(ctx, "SELECT question,answer,dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2 AND agent_id=$3", string(scope.OwnerID), history[i].ID, agent.ID).Scan(&question, &answer, &refs); err != nil {
				return memory.ErrNotFound
			}
			history[i].Question = question
			if verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: agent.ID, ContextVersions: refs}) == nil {
				history[i].Answer = answer
				dependencies = append(dependencies, refs...)
			}
		}
		var refs []memory.Ref
		memories, tasks, settings, refs, err = s.deskContextTx(ctx, tx, scope, agent, false)
		dependencies = append(dependencies, refs...)
		return err
	})
	if err != nil {
		return out, err
	}
	// Retrieval also uses the stored questions, never the browser's claimed
	// version of an ID-backed turn. Tombstones contain no deleted text.
	earlier := ""
	for _, turn := range history {
		earlier += turn.Question + " "
	}
	recall, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, memory.RecallRequest{Query: tail(earlier+question, 4000), Mode: "remember", Context: memory.WorkingContext{Objects: []memory.ID{}}, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}})
	if err != nil {
		return out, err
	}
	visible := map[string]workspace.Memory{}
	for _, m := range memories {
		if oneOf(m.Kind, agent.MemoryKinds...) && (m.Trust != "inferred" || agent.IncludeInferred) {
			visible[m.ID] = m
		}
	}
	loc := deskLocation(settings)
	var prompt strings.Builder
	prompt.WriteString(deskNow(loc))
	if settings.City != "" {
		fmt.Fprintf(&prompt, "用户所在城市：%s（问天气、附近等没说地点时默认用它）\n", settings.City)
	} else {
		fmt.Fprintln(&prompt, "用户所在城市：未设置（需要地点而用户没说时，可以按时区推断并说明，或请用户说城市）")
	}
	if len(history) > 0 {
		fmt.Fprintln(&prompt, "\n同一张卡片上之前的对话（本次是接着问）：")
		for _, turn := range history {
			fmt.Fprintf(&prompt, "问：%s\n", turn.Question)
			if turn.Answer != "" {
				fmt.Fprintf(&prompt, "答：%s\n", turn.Answer)
			} else if turn.ID != "" {
				fmt.Fprintln(&prompt, "（先前回答的依据已变化，请按当前允许的资料重新回答。）")
			}
		}
	}
	fmt.Fprintf(&prompt, "\n问题：%s\n\n检索到的记录（引用 ID）：\n", question)
	sent := map[string]memory.Ref{}
	contextClaims := []evidenceContextClaim{}
	for _, ref := range recall.Memories {
		m, ok := visible[string(ref.ID)]
		if !ok || sent[m.ID] != (memory.Ref{}) || len(sent) >= 20 {
			continue
		}
		fmt.Fprintf(&prompt, "[%s / %s / trust=%s / confirmation=%s / acquisition=%s] %s\n", m.ID, m.Epistemic, m.Trust, m.Confirmation, m.Acquisition, m.Text)
		sent[m.ID] = memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		dependencies = append(dependencies, sent[m.ID])
		contextClaims = append(contextClaims, evidenceContextClaim{Label: m.ID, Ref: sent[m.ID], Text: m.Text})
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := s.startUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ranked := []workspace.Memory{}
		for _, ref := range recall.Memories {
			if m, ok := visible[string(ref.ID)]; ok {
				ranked = append(ranked, m)
			}
		}
		if err := s.finishUseContextTx(ctx, tx, scope, agent, nil, question, ranked, &u); err != nil {
			return err
		}
		u.Supplemental = nil
		writeUseContext(&prompt, u, loc, func(m workspace.Memory) {
			fmt.Fprintf(&prompt, "[%s] %s\n", m.ID, m.Text)
			sent[m.ID] = memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		})
		dependencies = append(dependencies, u.Dependencies...)
		return nil
	}); err != nil {
		return out, err
	}
	// History turns repeat their own dependencies; keep the stored list a set.
	dependencies = uniqueRefs(dependencies)
	if len(sent) == 0 {
		fmt.Fprintln(&prompt, "（没有）")
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		groups, gaps, err := evidenceContextsTx(ctx, tx, scope, agent.ID, nil, contextClaims)
		if err != nil {
			return err
		}
		if len(groups) > 0 || len(gaps) > 0 {
			fmt.Fprintln(&prompt, "\n记忆的来源上下文：\n"+evidenceContextInstructions)
		}
		for _, group := range groups {
			dependencies = append(dependencies, group.Window.ProofRefs...)
			fmt.Fprintf(&prompt, "记录 %s 的原对话片段：\n", group.Label)
			for i, message := range group.Window.Messages {
				prompt.WriteString(contextMessageLine(fmt.Sprintf("上下文 %d", i+1), message, loc))
				dependencies = append(dependencies, message.Ref)
			}
			writeContextGaps(&prompt, group.Label, group.Window.Gaps)
		}
		writeContextGaps(&prompt, "范围提示", gaps)
		return nil
	}); err != nil {
		return out, err
	}
	dependencies = uniqueRefs(dependencies)
	fmt.Fprintln(&prompt, "\n未完成的事项：")
	for _, t := range tasks {
		fmt.Fprintf(&prompt, "- %s（%s", t.Title, t.Status)
		if t.Due != "" {
			fmt.Fprintf(&prompt, "，截止 %s", t.Due)
		}
		fmt.Fprintln(&prompt, "）")
	}
	if len(tasks) == 0 {
		fmt.Fprintln(&prompt, "（没有）")
	}
	p, _ := s.models.Get(agent.ID)
	checkContext := func() error {
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			err := s.checkSecretaryUseContextTx(ctx, tx, scope, secretaryContext{Agent: agent, Dependencies: dependencies})
			if errors.Is(err, memory.ErrForbidden) {
				return memory.ErrConflict
			}
			return err
		})
	}

	if err := checkContext(); err != nil {
		return out, err
	}
	reservationID, err := s.reserveModelCostID(ctx, scope.OwnerID, p.Reserve(deskInstructions+prompt.String()), nil)
	if err != nil {
		return out, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := s.models.GenerateWithSearch(workCtx, agent.ID, deskInstructions, prompt.String())
	cost := result.Cost
	if err != nil && strings.TrimSpace(result.Text) == "" {
		cost = 0
	}
	if settleErr := s.settleModelCost(ctx, scope.OwnerID, reservationID, cost); settleErr != nil {
		return out, settleErr
	}
	if err != nil {
		return out, fmt.Errorf("%w: %w", memory.ErrUnavailable, err)
	}
	turnID := string(memory.NewID())
	if err := s.recordReturnedUsage(ctx, result.Text, modelUsage{
		OwnerID: scope.OwnerID, ID: memory.NewID(), At: time.Now().UTC(),
		Purpose: "answer", AgentID: agent.ID, Model: p.Model,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, Cost: result.Cost,
		TurnID: turnID, MemoryRefs: dependencies,
	}); err != nil {
		return out, err
	}
	if err := checkContext(); err != nil {
		return out, err
	}
	var reply struct {
		Answer string   `json:"answer"`
		Used   []string `json:"used"`
		Links  []string `json:"links"`
	}
	text := strings.TrimSpace(result.Text)
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```"))
	if strictJSON([]byte(text), &reply) != nil || strings.TrimSpace(reply.Answer) == "" {
		// A model that ignored the format still answered; show it without sources.
		reply.Answer, reply.Used, reply.Links = strings.TrimSpace(result.Text), nil, nil
	}
	out = workspace.DeskAnswer{ID: turnID, Answer: strings.TrimSpace(reply.Answer), Agent: agent.Name, Used: []workspace.DeskSource{}, Searches: []string{}, Links: []string{}}
	if len(result.Searches) > 0 {
		out.Searches = append([]string{}, result.Searches...)
	}
	// Links become anchors on the page: web addresses only, a few, short.
	for _, link := range reply.Links {
		u, err := url.Parse(link)
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(link) <= 500 {
			out.Links = append(out.Links, u.String())
		}
	}
	for _, id := range reply.Used {
		// Only records that were actually sent can be cited.
		if ref, ok := sent[id]; ok {
			out.Used = append(out.Used, workspace.DeskSource{Ref: ref, Text: visible[id].Text})
			delete(sent, id)
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.checkSecretaryUseContextTx(ctx, tx, scope, secretaryContext{Agent: agent, Dependencies: dependencies}); err != nil {
			if errors.Is(err, memory.ErrForbidden) {
				return memory.ErrConflict
			}
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO desk_turns(owner_id,id,agent_id,question,answer,dependencies) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), out.ID, agent.ID, question, out.Answer, asJSON(dependencies))
		return err
	})
	if err != nil {
		return workspace.DeskAnswer{}, err
	}
	return out, nil
}

// A returned model response incurred usage even if later validation, lease
// checks or the answer transaction fail. Commit it once, independently of the
// answer, and finish that short write if the caller disconnects. Text is only
// checked for presence here; it is never included in the usage record.
func (s *Store) recordReturnedUsage(ctx context.Context, text string, usage modelUsage) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return pgx.BeginFunc(persistCtx, s.pool, func(tx pgx.Tx) error {
		return recordUsageTx(persistCtx, tx, usage)
	})
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
	memories, err := s.memoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true)
	if err != nil {
		return nil, nil, workspace.Settings{}, nil, err
	}
	tasks, settings, refs, err := s.deskItemsContextTx(ctx, tx, scope, agent, stable)
	return memories, tasks, settings, refs, err
}

// Secretary recall supplies the memory identities later; the legacy answer
// still needs its complete memory context. Share only the item/settings reads.
func (s *Store) deskItemsContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, stable bool) ([]workspace.Item, workspace.Settings, []memory.Ref, error) {
	order := "due_at NULLS LAST, updated_at DESC"
	if stable {
		order = "created_at,id"
	}
	tasks, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') ORDER BY "+order+" LIMIT 40", string(scope.OwnerID))
	if err != nil {
		return nil, workspace.Settings{}, nil, err
	}
	dependencies := []memory.Ref{}
	for i := range tasks {
		var refs []memory.Ref
		tasks[i], refs, err = sanitizeItemTx(ctx, tx, scope, agent.ID, tasks[i])
		if err != nil {
			return nil, workspace.Settings{}, nil, err
		}
		dependencies = append(dependencies, refs...)
	}
	settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
	return tasks, settings, dependencies, err
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

const recallTimeRelaxed = "按这个时间没有找到，下面是其他时间说的，回答时说明实际日期"
const recallDateInstructions = "回忆类的问题按给出的日期回答；看到按时间没有找到的说明时，要说明实际是哪天说的。"

// Append only supplied metadata; old memory lines stay byte-for-byte intact.
func memoryPromptSuffix(m workspace.Memory, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var suffix strings.Builder
	if at, err := time.Parse(time.RFC3339Nano, m.ExpressedAt); err == nil {
		fmt.Fprintf(&suffix, " / 说于 %s", at.In(loc).Format("2006-01-02"))
	}
	if from, err := time.Parse(time.RFC3339Nano, m.EventFrom); err == nil {
		from = from.In(loc)
		date := ""
		switch m.EventPrecision {
		case "day":
			date = from.Format("2006-01-02")
		case "month":
			date = from.Format("2006-01")
		case "year":
			date = from.Format("2006")
		case "range":
			if to, err := time.Parse(time.RFC3339Nano, m.EventTo); err == nil && to.After(from) {
				date = from.Format("2006-01-02") + " 至 " + to.Add(-time.Nanosecond).In(loc).Format("2006-01-02")
			}
		}
		if date != "" {
			fmt.Fprintf(&suffix, " / 事件 %s", date)
		}
	}
	for _, mention := range m.Mentions {
		if oneOf(mention.Role, "place", "person") && strings.TrimSpace(mention.Name) != "" {
			fmt.Fprintf(&suffix, " / %s", mention.Name)
		}
	}
	return suffix.String()
}
