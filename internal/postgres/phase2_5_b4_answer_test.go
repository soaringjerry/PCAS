package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Only this decoder needs changing if the backend wraps memory sections in
// another prompt envelope. Assertions use original fixture text and public IDs.
func phase25B4Prompt(r phase25B234ModelRequest) string {
	var out []string
	for _, m := range r.Messages {
		if m.Role == "user" {
			var s string
			if json.Unmarshal(m.Content, &s) == nil {
				out = append(out, s)
			}
		}
	}
	return strings.Join(out, "\n")
}
func phase25B4ReplyJSON(result phase25B4Reply, check bool) phase25B234ModelReply {
	s, e := phase25B4ModelOutput(phase25B4DefaultAdapter(), result, check)
	if e != nil {
		return phase25B234ModelReply{status: 500}
	}
	return phase25B234ModelReply{content: s}
}
func (f *phase25B234Fixture) secretaryTurn(t *testing.T, text, tier string, includeInferred ...bool) workspace.DeskTurnResponse {
	t.Helper()
	agent := f.answerAgent(t)
	if len(includeInferred) > 0 {
		f.exec(t, `UPDATE workspace_agents SET document=jsonb_set(document,'{includeInferred}',to_jsonb($3::boolean)) WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, agent, includeInferred[0])
	}
	f.index(t)
	ctx := f.ctx
	if tier != "" {
		if phase25B4TierContext == nil {
			t.Fatal("WithMemoryTier adapter not bound after merge")
		}
		ctx = phase25B4TierContext(ctx, tier)
	}
	answer, err := f.store.DeskTurn(ctx, f.scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), Text: text, AgentID: agent})
	if err != nil {
		t.Fatal(err)
	}
	return answer
}
func phase25B4MustContain(t *testing.T, prompt string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt misses %q", s)
		}
	}
}
func phase25B4Section(t *testing.T, prompt, heading, needle string) {
	t.Helper()
	i := strings.Index(prompt, heading)
	if i < 0 {
		t.Errorf("missing section %s", heading)
		return
	}
	j := strings.Index(prompt, needle)
	if j < i {
		t.Errorf("%q not in %s section", needle, heading)
	}
	end := len(prompt)
	for _, h := range []string{"交接说明", "必须遵守的要求", "现状卡", "期限和固定安排", "补充记忆", "相关原话"} {
		if h == heading {
			continue
		}
		if next := strings.Index(prompt[i+len(heading):], h); next >= 0 {
			next += i + len(heading)
			if next < end {
				end = next
			}
		}
	}
	if j >= end {
		t.Errorf("%q falls after the end of %s section", needle, heading)
	}
}
func (f *phase25B234Fixture) tierUsage(t *testing.T, wantTier string, want map[string]int) {
	t.Helper()
	rows, err := f.db.Query(f.ctx, `SELECT purpose,tier,plan FROM model_usage WHERE owner_id=$1 ORDER BY at,id`, f.scope.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var purpose, tier string
		var plan json.RawMessage
		if err := rows.Scan(&purpose, &tier, &plan); err != nil {
			t.Fatal(err)
		}
		if purpose == "card" || purpose == "handover" {
			continue
		}
		seen[purpose]++
		if tier != wantTier {
			t.Errorf("usage %s tier=%s want %s", purpose, tier, wantTier)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for purpose, n := range want {
		if seen[purpose] != n {
			t.Errorf("usage %s=%d want %d", purpose, seen[purpose], n)
		}
	}
}
func TestPhase25B4_X4_1_FutureAndRecurringDeadlinesInSection(t *testing.T) {
	f := phase25B234NewFixture(t)
	textA, textB := "虚构周五截止白鹭月报。", "虚构每周二晚上有课。"
	g, refs := f.groupTexts(t, textA, textB, "虚构月报先写结论。")
	f.card(t, g, refs, false)
	f.deadline(t, refs[0], time.Now().AddDate(0, 0, 7))
	f.exec(t, `UPDATE deadlines SET title=$3 WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, refs[0].ID, textA)
	f.exec(t, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,recurrence,title,time_note) VALUES($1,$2,$3,$4,'recurring','每周二晚上','虚构晚课','')`, f.scope.OwnerID, memory.NewID(), refs[1].ID, refs[1].Version)
	model := f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		prompt := phase25B4Prompt(r)
		phase25B4Section(t, prompt, "期限和固定安排", "虚构晚课")
		phase25B4Section(t, prompt, "期限和固定安排", textA)
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构安排已列出。"}, false)
	})
	f.secretaryTurn(t, "帮我排一下下周", "")
	if len(model.calls()) != 1 {
		t.Errorf("default secretary calls=%d", len(model.calls()))
	}
	f.tierUsage(t, "light", map[string]int{"secretary": 1, "selfcheck": 0})
	f.assertRevisions(t, refs...)
}
func TestPhase25B4_X4_2_NamedProjectAndAliasMustBeSelected(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprintf("alias=%v", alias), func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g, refs := f.cardGroup(t, 3)
			key := "entity:" + g.EntityID
			f.card(t, g, refs, false)
			name := g.Name
			if alias {
				name = "虚构青鹭代号"
				f.exec(t, `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)`, f.scope.OwnerID, g.EntityID, name)
			}
			original := refsText(t, f, refs[0])
			f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
				p := phase25B4Prompt(r)
				phase25B4MustContain(t, p, g.Name, original)
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构项目建议。"}, false)
			})
			f.secretaryTurn(t, "请处理"+name, "")
			f.assertRevisions(t, refs...)
			var selected bool
			if err := f.db.QueryRow(f.ctx, `SELECT coalesce(bool_or(plan->'groups' ? $2),false) FROM model_usage WHERE owner_id=$1 AND purpose='secretary'`, f.scope.OwnerID, key).Scan(&selected); err != nil {
				t.Fatal(err)
			}
			if !selected {
				t.Error("named group absent from usage plan.groups")
			}
		})
	}
}
func refsText(t *testing.T, f *phase25B234Fixture, r memory.Ref) string {
	t.Helper()
	m, e := f.store.GetMemory(f.ctx, f.scope, string(r.ID))
	if e != nil {
		t.Fatal(e)
	}
	return m.Text
}
func TestPhase25B4_X4_3_ApplicableSelfRulesAndSectionOrder(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.groupTexts(t, "虚构邮件方案先写结论。", "虚构邮件主题使用月报。", "虚构邮件结尾写致谢。")
	f.card(t, g, refs, false)
	rules := []string{"虚构要求：发出去之前先给我看。", "虚构要求：不用夸张形容词。", "虚构要求：游泳时戴护目镜。"}
	var rs []memory.Ref
	for _, s := range rules {
		r := f.claim(t, s)
		f.labels(t, r, "rule", true, 1)
		rs = append(rs, r)
	}
	f.exec(t, `INSERT INTO status_cards(owner_id,key,kind,name,rule,built_at,stale) VALUES($1,'self:rule','self','虚构对助手的要求',1,now(),false)`, f.scope.OwnerID)
	for i, r := range rs {
		f.exec(t, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version,applies_to) VALUES($1,'self:rule','status',$2,$3,$4,$5)`, f.scope.OwnerID, i, r.ID, r.Version, []string{"起草邮件", "", "游泳"}[i])
	}
	f.handover(t, "entity:"+g.EntityID, false)
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		p := phase25B4Prompt(r)
		phase25B4Section(t, p, "必须遵守的要求", rules[0])
		phase25B4Section(t, p, "必须遵守的要求", rules[1])
		ordered := []string{"交接说明", "必须遵守的要求", "现状卡", "期限和固定安排", "补充记忆", "相关原话"}
		last := -1
		for _, h := range ordered {
			i := strings.Index(p, h)
			if i < 0 || i <= last {
				t.Errorf("section order %s at=%d previous=%d", h, i, last)
			}
			last = i
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构邮件草稿，请先查看。"}, false)
	})
	f.secretaryTurn(t, "起草白鹭月报邮件", "")
	f.assertRevisions(t, append(refs, rs...)...)
}
func TestPhase25B4_X4_4_ActualFallbackAnswer(t *testing.T) {
	f := phase25B234NewFixture(t)
	r := f.claim(t, "AcceptanceFallback 虚构便签：月报先写结论。")
	f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		phase25B4MustContain(t, phase25B4Prompt(request), "月报先写结论")
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构回退回答。", used: []string{"M1"}}, false)
	})
	turn := f.secretaryTurn(t, "AcceptanceFallback", "")
	if turn.Turn.Reply != "虚构回退回答。" {
		t.Errorf("fallback reply=%s", turn.Turn.Reply)
	}
	f.assertRevisions(t, r)
}
func TestPhase25B4_X4_5_AllCardItemsPersistAsDependencies(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.card(t, g, refs, false)
	originals := make([]string, len(refs))
	for i, r := range refs {
		originals[i] = refsText(t, f, r)
	}
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		p := phase25B4Prompt(r)
		for _, original := range originals {
			phase25B4MustContain(t, p, original)
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构引用卡片回复。", used: []string{"M1"}}, false)
	})
	turn := f.secretaryTurn(t, "请处理"+g.Name, "")
	f.assertAnswerOutdated(t, turn.ConversationID, false)
	refs[1] = f.correct(t, refs[1], "虚构改正未被 used 点名的卡片条目。")
	f.assertAnswerOutdated(t, turn.ConversationID, true)
	f.assertRevisions(t, refs...)
}
func TestPhase25B4_X4_6_MediumPromotionSameDataAndBilling(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%v", missing), func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g, refs := f.cardGroup(t, 3)
			f.card(t, g, refs, false)
			originals := make([]string, len(refs))
			for i, r := range refs {
				originals[i] = refsText(t, f, r)
			}
			var first string
			var firstMu sync.Mutex
			model := f.model(t, func(_ *http.Request, n int, r phase25B234ModelRequest) phase25B234ModelReply {
				p := phase25B4Prompt(r)
				if n == 1 {
					time.Sleep(50 * time.Millisecond) // Deterministic budget for the real selfcheck deadline.
					firstMu.Lock()
					first = p
					firstMu.Unlock()
					return phase25B4ReplyJSON(phase25B4Reply{text: "虚构初稿。", missingKeyInformation: missing}, false)
				}
				firstMu.Lock()
				initial := first
				firstMu.Unlock()
				for i, ref := range refs {
					s := originals[i]
					if strings.Contains(initial, s) && !strings.Contains(p, s) {
						t.Errorf("selfcheck lacks original memory %s", ref.ID)
					}
				}
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构检查后的回复。"}, true)
			})
			question := "仔细查一下" + g.Name
			if missing {
				question = "处理" + g.Name
			}
			turn := f.secretaryTurn(t, question, "")
			if len(model.calls()) != 2 {
				t.Errorf("medium calls=%d", len(model.calls()))
			}
			if turn.Turn.Reply != "虚构检查后的回复。" {
				t.Errorf("checked reply=%q", turn.Turn.Reply)
			}
			f.tierUsage(t, "medium", map[string]int{"secretary": 1, "selfcheck": 1})
			f.assertRevisions(t, refs...)
		})
	}
}
func TestPhase25B4_X4_7_SelfcheckFailureKeepsDraftWithoutRetry(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.card(t, g, refs, false)
	model := f.model(t, func(_ *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
		if n == 1 {
			time.Sleep(50 * time.Millisecond)
			return phase25B4ReplyJSON(phase25B4Reply{text: "虚构可用初稿。"}, false)
		}
		return phase25B234ModelReply{status: 503}
	})
	turn := f.secretaryTurn(t, "认真检查虚构事项", "")
	if turn.Turn.Reply != "虚构可用初稿。" {
		t.Errorf("failure lost draft=%q", turn.Turn.Reply)
	}
	if len(model.calls()) != 2 {
		t.Errorf("selfcheck retried calls=%d", len(model.calls()))
	}
}
func TestPhase25B4_X4_8_SelfcheckCannotAddDeletion(t *testing.T) {
	t.Skip("finding F-B4-4")
	for _, added := range []bool{false, true} {
		t.Run(fmt.Sprintf("added_by_selfcheck=%v", added), func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g, refs := f.cardGroup(t, 3)
			f.card(t, g, refs, false)
			taskID := f.task(t, "虚构不可删事项")
			deletion := phase25B4DeleteAction("T1")
			model := f.model(t, func(_ *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
				var actions []json.RawMessage
				if !added || n > 1 {
					actions = []json.RawMessage{deletion}
				}
				if n == 1 {
					time.Sleep(50 * time.Millisecond)
					return phase25B4ReplyJSON(phase25B4Reply{text: "虚构保留事项。", actions: actions}, false)
				}
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构检查稿。", actions: actions}, true)
			})
			turn := f.secretaryTurn(t, "仔细检查虚构事项", "medium")
			if len(model.calls()) != 2 || turn.Turn.Reply != "虚构检查稿。" {
				t.Errorf("selfcheck did not run: calls=%d reply=%s", len(model.calls()), turn.Turn.Reply)
			}
			var exists bool
			if err := f.db.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)`, f.scope.OwnerID, taskID).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if added && !exists {
				t.Error("selfcheck invented deletion executed")
			}
			if !added && exists {
				t.Error("original deletion positive control did not execute; bind the valid model action format")
			}
		})
	}
}

func TestPhase25B4_X4_11_RetiredAbsentFromEveryPrompt(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 5)
	f.card(t, g, refs, false)
	oldDup := refsText(t, f, refs[3])
	oldSup := refsText(t, f, refs[4])
	f.retire(t, refs[3], refs[0], "duplicate")
	f.retire(t, refs[4], refs[0], "superseded")
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		p := phase25B4Prompt(r)
		if strings.Contains(p, oldDup) || strings.Contains(p, oldSup) {
			t.Error("retired original text leaked into a prompt section")
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构仅用当前记忆。"}, false)
	})
	f.secretaryTurn(t, "仔细处理"+g.Name, "")
	f.assertRevisions(t, refs...)
}
func TestPhase25B4_RandomPromptAndTierSequence(t *testing.T) {
	const seed int64 = 252504
	f := phase25B234NewFixtureTimeout(t, 3*time.Minute)
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed=%d", seed)
	g, refs := f.cardGroup(t, 8)
	var mu sync.RWMutex
	allowed := map[string]bool{}
	for _, r := range refs {
		allowed[refsText(t, f, r)] = true
	}
	forbidden := map[string]bool{}
	model := f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		if phase25B4Kind(r) == "answer" {
			time.Sleep(50 * time.Millisecond)
		}
		p := phase25B4Prompt(r)
		mu.RLock()
		defer mu.RUnlock()
		for s := range forbidden {
			if strings.Contains(p, s) {
				t.Errorf("retired or superseded version in model content: %s", s)
			}
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构随机轮次回复。"}, false)
	})
	for step := 0; step < 24; step++ {
		i := 3 + rng.Intn(len(refs)-3)
		switch rng.Intn(3) {
		case 0:
			old := refsText(t, f, refs[i])
			refs[i] = f.correct(t, refs[i], fmt.Sprintf("虚构随机提示词新版本 %d。", step))
			mu.Lock()
			delete(allowed, old)
			forbidden[old] = true
			allowed[refsText(t, f, refs[i])] = true
			mu.Unlock()
		case 1:
			old := refsText(t, f, refs[i])
			f.retire(t, refs[i], refs[0], "superseded")
			mu.Lock()
			delete(allowed, old)
			forbidden[old] = true
			mu.Unlock()
			refs = append(refs[:i], refs[i+1:]...)
		case 2:
			old := refsText(t, f, refs[i])
			if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[i]}}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			delete(allowed, old)
			forbidden[old] = true
			mu.Unlock()
			refs = append(refs[:i], refs[i+1:]...)
		}
		// Keep sufficient live items for repeated invalidation and rebuilds.
		if len(refs) < 5 {
			r := f.claim(t, fmt.Sprintf("虚构随机提示词补充 %d。", step))
			f.labels(t, r, "progress", true, 1, g)
			refs = append(refs, r)
			mu.Lock()
			allowed[refsText(t, f, r)] = true
			mu.Unlock()
		}
		f.card(t, g, refs, false)
		question := "处理" + g.Name
		if step%2 == 1 {
			question = "仔细" + question
		}
		before := len(model.calls())
		f.secretaryTurn(t, question, "")
		wantCalls, wantTier := 1, "light"
		if step%2 == 1 {
			wantCalls, wantTier = 2, "medium"
		}
		if got := len(model.calls()) - before; got != wantCalls {
			t.Errorf("step=%d calls=%d want=%d", step, got, wantCalls)
		}
		var tier string
		if err := f.db.QueryRow(f.ctx, `SELECT tier FROM model_usage WHERE owner_id=$1 AND purpose='secretary' ORDER BY at DESC LIMIT 1`, f.scope.OwnerID).Scan(&tier); err != nil {
			t.Fatal(err)
		}
		if tier != wantTier {
			t.Errorf("step=%d tier=%s want=%s", step, tier, wantTier)
		}
		f.assertRevisions(t, refs...)
		if t.Failed() {
			t.Fatalf("seed=%d step=%d", seed, step)
		}
	}
}

// The heavy/deputy adapter below exercises the existing real run boundary. It
// seeds a disposable pending work item/run, then lets RunAgents lease/process
// it; no completion result or dependency is inserted by the test itself.
func (f *phase25B234Fixture) deputyRun(t *testing.T, question string) workspace.Run {
	t.Helper()
	agent := f.answerAgent(t)
	thing, run := f.task(t, "虚构副手任务"), string(memory.NewID())
	document, _ := json.Marshal(workspace.Run{ID: run, ThingID: thing, AgentID: agent, Kind: "draft", Prompt: question, Status: "queued", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	f.exec(t, `INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,document,created_at) VALUES($1,$2,$3,$4,'queued',0,$5,now())`, f.scope.OwnerID, run, thing, agent, document)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		done <- f.store.RunAgents(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() {
		cancel()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Error("owned deputy worker did not stop")
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status string
		var doc json.RawMessage
		if err := f.db.QueryRow(f.ctx, `SELECT status,document FROM agent_runs WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, run).Scan(&status, &doc); err != nil {
			t.Fatal(err)
		}
		if status == "done" || status == "completed" || status == "failed" {
			var result workspace.Run
			if err := json.Unmarshal(doc, &result); err != nil {
				t.Fatal(err)
			}
			if status == "failed" {
				t.Fatalf("deputy failed: %+v", result)
			}
			return result
		}
		select {
		case err := <-done:
			t.Fatalf("deputy worker exited before completion: %v", err)
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestPhase25B4_NamedGroupsRespectSixCardLimit(t *testing.T) {
	f := phase25B234NewFixture(t)
	groups := f.heavyGroups(t, 7)
	var names []string
	for i := range groups {
		names = append(names, fmt.Sprintf("虚构接力项目%02d", i))
	}
	f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构六卡上限回复。"}, false)
	})
	f.secretaryTurn(t, "处理 "+strings.Join(names, "、"), "")
	var raw json.RawMessage
	if err := f.db.QueryRow(f.ctx, `SELECT plan->'groups' FROM model_usage WHERE owner_id=$1 AND purpose='secretary' ORDER BY at DESC LIMIT 1`, f.scope.OwnerID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 6 {
		t.Errorf("seven named groups selected=%d want 6", len(keys))
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			t.Error("duplicate selected group")
		}
		seen[key] = true
		found := false
		for _, g := range groups {
			if key == g.key {
				found = true
			}
		}
		if !found {
			t.Errorf("selected unnamed group=%s", key)
		}
	}
}
func TestPhase25B4_MainRetryAtMostTwiceWithinThirtySeconds(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.card(t, g, refs, false)
	model := f.model(t, func(_ *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
		if n == 1 {
			return phase25B234ModelReply{status: 503}
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构第二次主调用成功。"}, false)
	})
	start := time.Now()
	turn := f.secretaryTurn(t, "虚构普通问题", "")
	if len(model.calls()) == 0 || len(model.calls()) > 2 {
		t.Errorf("main attempts=%d want 1..2", len(model.calls()))
	}
	if time.Since(start) > 30*time.Second {
		t.Error("main exceeded thirty seconds")
	}
	if len(model.calls()) == 2 && turn.Turn.Reply != "虚构第二次主调用成功。" {
		t.Errorf("retried reply=%s", turn.Turn.Reply)
	}
}
func TestPhase25B4_SelfcheckUsesActualDraftDurationAndKeepsDraft(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.card(t, g, refs, false)
	model := f.model(t, func(r *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
		if n == 1 {
			select {
			case <-time.After(150 * time.Millisecond):
			case <-r.Context().Done():
			}
			return phase25B4ReplyJSON(phase25B4Reply{text: "虚构快速初稿。"}, false)
		}
		select {
		case <-r.Context().Done():
			return phase25B234ModelReply{status: 503}
		case <-time.After(5 * time.Second):
			t.Error("selfcheck ignored actual draft-duration budget")
			return phase25B4ReplyJSON(phase25B4Reply{text: "虚构迟到修订。"}, true)
		}
	})
	start := time.Now()
	turn := f.secretaryTurn(t, "仔细看看虚构事项", "")
	if time.Since(start) > 2*time.Second {
		t.Errorf("selfcheck timeout too long: %s", time.Since(start))
	}
	if len(model.calls()) != 2 {
		t.Errorf("selfcheck attempts=%d", len(model.calls()))
	}
	if turn.Turn.Reply != "虚构快速初稿。" {
		t.Errorf("timeout lost draft=%s", turn.Turn.Reply)
	}
}
func TestPhase25B4_SelfcheckSkipRemovesOriginalAction(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.card(t, g, refs, false)

	control := f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构新建正控制。", actions: []json.RawMessage{json.RawMessage(`{"op":"create_task","title":"虚构动作正控制"}`)}}, false)
	})
	f.secretaryTurn(t, "新建虚构动作正控制", "light")
	var created int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM work_items WHERE owner_id=$1 AND title='虚构动作正控制'`, f.scope.OwnerID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(control.calls()) != 1 {
		t.Fatalf("original create action not valid: count=%d calls=%d", created, len(control.calls()))
	}
	model := f.model(t, func(_ *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
		if n == 1 {
			time.Sleep(50 * time.Millisecond)
			return phase25B4ReplyJSON(phase25B4Reply{text: "虚构初稿。", actions: []json.RawMessage{json.RawMessage(`{"op":"create_task","title":"虚构应被撤掉的事项"}`)}}, false)
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构撤掉动作。", actions: []json.RawMessage{json.RawMessage(`{"op":"skip"}`)}}, true)
	})
	turn := f.secretaryTurn(t, "认真处理虚构事项", "")
	if len(model.calls()) != 2 || turn.Turn.Reply != "虚构撤掉动作。" {
		t.Errorf("skip selfcheck not applied: calls=%d reply=%s", len(model.calls()), turn.Turn.Reply)
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM work_items WHERE owner_id=$1 AND title='虚构应被撤掉的事项'`, f.scope.OwnerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("skipped original action executed")
	}
}

func phase25B4Region(t *testing.T, prompt, heading string) string {
	t.Helper()
	i := strings.Index(prompt, heading)
	if i < 0 {
		t.Errorf("missing section %s", heading)
		return ""
	}
	i += len(heading)
	end := len(prompt)
	for _, next := range []string{"交接说明", "必须遵守的要求", "现状卡", "期限和固定安排", "补充记忆", "相关原话"} {
		if next == heading {
			continue
		}
		if j := strings.Index(prompt[i:], next); j >= 0 && i+j < end {
			end = i + j
		}
	}
	return prompt[i:end]
}
func TestPhase25B4_TwelveRulesFifteenDeadlinesAndSupplementLimits(t *testing.T) {
	f := phase25B234NewFixtureTimeout(t, 3*time.Minute)
	g, cardRefs := f.cardGroup(t, 3)
	f.card(t, g, cardRefs, false)
	var rules, deadlines, supplement []memory.Ref
	for i := 0; i < 15; i++ {
		r := f.claim(t, fmt.Sprintf("MustRule%02d 虚构要求：用中文简洁表达。", i))
		f.labels(t, r, "rule", true, 1)
		rules = append(rules, r)
	}
	f.exec(t, `INSERT INTO status_cards(owner_id,key,kind,name,rule,built_at,stale) VALUES($1,'self:rule','self','虚构对助手的要求',1,now(),false)`, f.scope.OwnerID)
	for i, r := range rules {
		f.exec(t, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version,applies_to) VALUES($1,'self:rule','status',$2,$3,$4,'')`, f.scope.OwnerID, i, r.ID, r.Version)
	}
	for i := 0; i < 16; i++ {
		r := f.claim(t, fmt.Sprintf("虚构未来安排%02d。", i))
		deadlines = append(deadlines, r)
		f.deadline(t, r, time.Now().Add(24*time.Hour+time.Duration(i)*time.Hour))
		f.exec(t, `UPDATE deadlines SET title=$3 WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, r.ID, fmt.Sprintf("AcceptanceDeadline%02d", i))
	}
	for i := 0; i < 20; i++ {
		r := f.claim(t, fmt.Sprintf("AcceptanceSupplementNote%02d 虚构白鹭月报补充记忆。", i))
		supplement = append(supplement, r)
	}
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		p := phase25B4Prompt(r)
		if n := strings.Count(phase25B4Region(t, p, "必须遵守的要求"), "MustRule"); n != 12 {
			t.Errorf("unlimited applicable rules=%d want 12", n)
		}
		deadlineBody := phase25B4Region(t, p, "期限和固定安排")
		if n := strings.Count(deadlineBody, "AcceptanceDeadline"); n != 15 {
			t.Errorf("future deadlines=%d want 15", n)
		}
		if strings.Contains(deadlineBody, "AcceptanceDeadline15") {
			t.Error("latest sixteenth deadline displaced nearer deadline")
		}
		if n := strings.Count(phase25B4Region(t, p, "补充记忆"), "AcceptanceSupplementNote"); n == 0 || n > 15 {
			t.Errorf("supplement count=%d want 1..15", n)
		}
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构分节上限回复。"}, false)
	})
	f.secretaryTurn(t, g.Name+" AcceptanceSupplementNote", "")
	f.assertRevisions(t, append(append(append(cardRefs, rules...), deadlines...), supplement...)...)
}

func (f *phase25B234Fixture) task(t *testing.T, title string) string {
	t.Helper()
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.store.Execute(f.ctx, f.scope, workspace.Command{Type: "addTask", Kind: "task", Title: title, Text: title, Status: "todo", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range state.Tasks {
		if task.Title == title {
			return task.ID
		}
	}
	t.Fatal("public task creation returned no task")
	return ""
}
