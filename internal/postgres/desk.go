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
		// The same visibility as a run: only what this assistant has been granted.
		if memories, err = s.memoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true); err != nil {
			return err
		}
		if tasks, err = queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') ORDER BY due_at NULLS LAST, updated_at DESC LIMIT 40", string(scope.OwnerID)); err != nil {
			return err
		}
		for i := range tasks {
			var refs []memory.Ref
			tasks[i], refs, err = sanitizeItemTx(ctx, tx, scope, agent.ID, tasks[i])
			if err != nil {
				return err
			}
			dependencies = append(dependencies, refs...)
		}
		settings, err = queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
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
		if oneOf(m.Kind, agent.MemoryKinds...) && (m.Epistemic != "inferred" || agent.IncludeInferred) {
			visible[m.ID] = m
		}
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		loc = time.UTC
	}
	var prompt strings.Builder
	now := time.Now().In(loc)
	fmt.Fprintf(&prompt, "现在：%s 星期%s（%s）\n", now.Format("2006-01-02 15:04"), []string{"日", "一", "二", "三", "四", "五", "六"}[now.Weekday()], loc)
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
	for _, ref := range recall.Memories {
		m, ok := visible[string(ref.ID)]
		if !ok || sent[m.ID] != (memory.Ref{}) || len(sent) >= 20 {
			continue
		}
		fmt.Fprintf(&prompt, "[%s / %s / confirmation=%s / acquisition=%s] %s\n", m.ID, m.Epistemic, m.Confirmation, m.Acquisition, m.Text)
		sent[m.ID] = memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		dependencies = append(dependencies, sent[m.ID])
	}
	if len(sent) == 0 {
		fmt.Fprintln(&prompt, "（没有）")
	}
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
			if err := verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: agent.ID, ContextVersions: dependencies}); err != nil {
				return err
			}
			// Excluding a task's input can change its permitted title without
			// changing a memory version or its global grant.
			for _, sentTask := range tasks {
				current, err := getItem(ctx, tx, scope, sentTask.ID)
				if err != nil {
					return memory.ErrConflict
				}
				current, _, err = sanitizeItemTx(ctx, tx, scope, agent.ID, current)
				if err != nil {
					return err
				}
				if current.Title != sentTask.Title || current.Status != sentTask.Status || current.Due != sentTask.Due {
					return memory.ErrConflict
				}
			}
			return nil
		})
	}
	if err := checkContext(); err != nil {
		return out, err
	}
	if err := s.reserveModelCost(ctx, scope.OwnerID, p.Reserve(deskInstructions+prompt.String()), nil); err != nil {
		return out, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := s.models.GenerateWithSearch(workCtx, agent.ID, deskInstructions, prompt.String())
	if err != nil {
		return out, fmt.Errorf("%w: %w", memory.ErrUnavailable, err)
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
	out = workspace.DeskAnswer{Answer: strings.TrimSpace(reply.Answer), Agent: agent.Name, Used: []workspace.DeskSource{}, Searches: []string{}, Links: []string{}}
	if len(result.Searches) > 0 {
		out.Searches = result.Searches[:min(len(result.Searches), 5)]
	}
	// Links become anchors on the page: web addresses only, a few, short.
	for _, link := range reply.Links {
		u, err := url.Parse(link)
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(link) <= 500 && len(out.Links) < 3 {
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
	out.ID = string(memory.NewID())
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := verifyRunTx(ctx, tx, scope, workspace.Run{AgentID: agent.ID, ContextVersions: dependencies}); err != nil {
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
