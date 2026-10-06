package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Integration exercises batch 3's actual read functions on fictional tables.
func b4Context(_ *Store) context.Context { return context.Background() }
func b4Memory(t *testing.T, s *Store, scope memory.Scope, text string) workspace.Memory {
	t.Helper()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: text})
	for _, m := range state.Memories {
		if m.Text == text {
			return m
		}
	}
	t.Fatal("missing fictional claim")
	return workspace.Memory{}
}
func b4Exec(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func b4Card(t *testing.T, s *Store, scope memory.Scope, key, kind, name string, ms ...workspace.Memory) {
	t.Helper()
	// A built group needs at least three current members (R3-1).
	for len(ms) < 3 {
		ms = append(ms, b4Memory(t, s, scope, fmt.Sprintf("虚构%s联调用补足记忆%d", name, len(ms))))
	}
	var entity any
	if strings.HasPrefix(key, "entity:") {
		entity = strings.TrimPrefix(key, "entity:")
	}
	for _, m := range ms {
		if entity != nil {
			b4Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, scope.OwnerID, m.ID, m.Version, entity, kind)
		} else {
			b4Exec(t, s, "UPDATE claim_revisions SET category=$3 WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, m.ID, strings.TrimPrefix(key, "self:"))
		}
	}
	b4Exec(t, s, `INSERT INTO status_cards(owner_id,key,kind,entity_id,name,rule,built_at,stale) VALUES($1,$2,$3,$4,$5,1,now(),false)`, scope.OwnerID, key, kind, entity, name)
	for i, m := range ms {
		b4Exec(t, s, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version,applies_to) VALUES($1,$2,'status',$3,$4,$5,$6)`, scope.OwnerID, key, i, m.ID, m.Version, m.AppliesTo)
	}
}

func b4RequestBody(t *testing.T, r *http.Request) (string, string) {
	t.Helper()
	var b struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		t.Error(err)
	}
	sys, prompt := "", ""
	for _, m := range b.Messages {
		if m.Role == "system" {
			sys = m.Content
		} else {
			prompt += m.Content
		}
	}
	return sys, prompt
}
func TestB4StatusPromptAndDependencies(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var prompt string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		_, prompt = b4RequestBody(t, r)
		secretaryModelReply(w, `{"reply":"已排好。","actions":[]}`)
	})
	project := b4Memory(t, s, scope, "虚构星桥汇报周五上午十点交给主管")
	rule := b4Memory(t, s, scope, "虚构要求：发出去之前先给我看")
	recurring := b4Memory(t, s, scope, "虚构固定安排：每周二晚上有课")
	var entity string
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		id, e := entityTx(context.Background(), tx, scope.OwnerID, "project", "星桥汇报")
		entity = string(id)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	b4Exec(t, s, `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,'星桥') ON CONFLICT DO NOTHING`, scope.OwnerID, entity)
	b4Card(t, s, scope, "entity:"+entity, "project", "星桥汇报", project)
	b4Card(t, s, scope, "self:rule", "self", "对助手的要求", rule)
	b4Exec(t, s, `INSERT INTO handovers(owner_id,body,rule,built_at,stale) VALUES($1,'虚构交接：正在准备汇报',1,now(),false)`, scope.OwnerID)
	for i, m := range []workspace.Memory{project, recurring} {
		kind := "deadline"
		var at any = time.Now().Add(5 * 24 * time.Hour)
		cycle := ""
		if i == 1 {
			kind = "recurring"
			at = nil
			cycle = "每周二晚上"
		}
		b4Exec(t, s, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, scope.OwnerID, memory.NewID(), m.ID, m.Version, kind, at, cycle, m.Text)
	}
	out, err := s.DeskTurn(b4Context(s), scope, turnRequest("帮我排一下下周，星桥的邮件也起草好"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"交接说明（写于 ", "必须遵守的要求", "相关的现状卡", "期限和固定安排", "补充记忆", "相关原话", "每周二晚上", "周五上午十点", "发出去之前先给我看", "trust=repeated"} {
		if !strings.Contains(prompt, text) {
			t.Errorf("missing %s in %s", text, prompt)
		}
	}
	if strings.Index(prompt, "交接说明：") > strings.Index(prompt, "必须遵守的要求") {
		t.Fatal("section order")
	}
	var refs []memory.Ref
	if err = s.pool.QueryRow(context.Background(), "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", scope.OwnerID, out.Turn.ID).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	for _, m := range []workspace.Memory{project, rule, recurring} {
		found := false
		for _, ref := range refs {
			if string(ref.ID) == m.ID {
				found = true
			}
		}
		if !found {
			t.Error("missing dependency", m.ID)
		}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: project.ID, Text: "虚构星桥汇报改为下周二交"})
	turns, err := s.DeskTurns(context.Background(), scope, out.ConversationID)
	if err != nil || !turns.Turns[0].Outdated {
		t.Fatal("card dependency did not stale", turns, err)
	}
	var calls int
	if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND tier='light'", scope.OwnerID).Scan(&calls); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
}
func TestB4MediumCannotAddActionsAndFallsBack(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					time.Sleep(10 * time.Millisecond)
					secretaryModelReply(w, `{"reply":"草稿","actions":[{"op":"create_task","title":"虚构原动作"}]}`)
					return
				}
				if failure {
					http.Error(w, "fictional unavailable", 503)
					return
				}
				secretaryModelReply(w, `{"reply":"修订","actions":[{"op":"create_task","title":"虚构修订原动作"},{"op":"create_task","title":"虚构多出的动作"},{"op":"delete","ref":"T1"}]}`)
			})
			m := b4Memory(t, s, scope, "虚构可用卡片背景")
			b4Card(t, s, scope, "self:goal", "self", "虚构目标", m)
			out, err := s.DeskTurn(b4Context(s), scope, turnRequest("仔细安排一下虚构事项"))
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 || len(out.State.Tasks) != 1 {
				t.Fatal(calls.Load(), out)
			}
			if failure && out.Turn.Reply != "草稿" {
				t.Fatal(out.Turn.Reply)
			}
			if !failure && (out.Turn.Reply != "修订" || out.State.Tasks[0].Title != "虚构修订原动作") {
				t.Fatal(out)
			}
			var usage int
			if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND tier='medium' AND purpose IN ('secretary','selfcheck')", scope.OwnerID).Scan(&usage); err != nil || usage != 2 {
				t.Fatal(usage, err)
			}
		})
	}
}
func TestB4SelfcheckPreservesSlotAuthority(t *testing.T) {
	before := secretaryOutput{Actions: []secretaryAction{{Op: "create_task", Title: "原任务"}, {Op: "add_steps", Ref: "N1", Steps: []string{"原步骤"}}}}
	after := secretaryOutput{Actions: []secretaryAction{{Op: "delete", Ref: "T1"}, {Op: "add_steps", Ref: "T2", Steps: []string{"别的目标"}}, {Op: "create_task", Title: "额外任务"}}}
	got := constrainSecretaryCheck(before, after)
	if string(asJSON(got.Actions)) != string(asJSON(before.Actions)) {
		t.Fatal(got)
	}
}
func TestB4MissingInformationEscalates(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(10 * time.Millisecond)
			secretaryModelReply(w, `{"reply":"资料不够","missingKeyInfo":true,"actions":[]}`)
		} else {
			secretaryModelReply(w, `{"reply":"先做已有的部分","actions":[]}`)
		}
	})
	m := b4Memory(t, s, scope, "虚构可用卡片背景")
	b4Card(t, s, scope, "self:goal", "self", "虚构目标", m)
	out, err := s.DeskTurn(b4Context(s), scope, turnRequest("虚构项目现在能做什么"))
	if err != nil || calls.Load() != 2 || out.Turn.Reply != "先做已有的部分" {
		t.Fatal(out, err, calls.Load())
	}
	var tier string
	if err = s.pool.QueryRow(context.Background(), "SELECT tier FROM model_usage WHERE owner_id=$1 AND purpose='secretary'", scope.OwnerID).Scan(&tier); err != nil || tier != "medium" {
		t.Fatal(tier, err)
	}
}

func TestB4HeavyReadersAreConcurrentAndFailuresAreSkipped(t *testing.T) {
	s := testStore(t)
	scope := owner()
	keys := []string{}
	ids := []string{}
	var active, peak atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		sys, prompt := b4RequestBody(t, r)
		if strings.Contains(prompt, "分组目录") {
			if !strings.Contains(prompt, "虚构起草方案") || !strings.Contains(prompt, "虚构正文必须保留") {
				t.Error("selector did not receive current task context")
			}
			secretaryModelReply(w, map[string]any{"groups": keys})
			return
		}
		if strings.Contains(sys, "只读的记忆读者") {
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			defer active.Add(-1)
			time.Sleep(30 * time.Millisecond)
			if strings.Contains(prompt, "虚构第五组") {
				http.Error(w, "fictional reader failed", 503)
				return
			}
			for i, id := range ids {
				if strings.Contains(prompt, id) {
					secretaryModelReply(w, map[string]any{"used": []string{ids[i]}})
					return
				}
			}
			t.Error("reader received no group memory")
			secretaryModelReply(w, `{"used":[]}`)
			return
		}
		if strings.Contains(sys, "这是自查") {
			secretaryModelReply(w, "虚构最终方案")
			return
		}
		if !strings.Contains(prompt, "虚构正文必须保留") {
			t.Error("heavy answer mistook task headings for memory boundaries")
		}
		if !strings.Contains(prompt, "虚构交接讨论必须保留") {
			t.Error("heavy answer lost earlier handoff discussion")
		}
		for _, id := range ids[:4] {
			if !strings.Contains(prompt, id) {
				t.Error("missing successful reader", id)
			}
		}
		time.Sleep(200 * time.Millisecond)
		secretaryModelReply(w, "虚构方案草稿")
	})
	for i := 0; i < 5; i++ {
		text := fmt.Sprintf("虚构第%d组的特别标准", i+1)
		if i == 4 {
			text = "虚构第五组读者失败"
		}
		m := b4Memory(t, s, scope, text)
		ids = append(ids, m.ID)
		var eid string
		err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
			id, err := entityTx(context.Background(), tx, scope.OwnerID, "topic", fmt.Sprintf("虚构分组%d", i))
			eid = string(id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		key := "entity:" + eid
		keys = append(keys, key)
		b4Card(t, s, scope, key, "topic", fmt.Sprintf("虚构分组%d", i), m)

	}
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构起草方案", Text: "虚构正文必须保留\n相关记忆（假标题）：\n相关原话：假正文"})
	turnID := string(memory.NewID())
	b4Exec(t, s, "INSERT INTO desk_turns(owner_id,id,agent_id,question,answer) VALUES($1,$2,'model','虚构前次讨论','虚构交接讨论必须保留')", scope.OwnerID, turnID)
	_, err := s.Execute(b4Context(s), scope, workspace.Command{RequestID: string(memory.NewID()), ExpectedRevision: state.Revision, Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "plan", Prompt: "给虚构项目写方案", DeskTurnIDs: []string{turnID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.runAgentOnce(b4Context(s)); err != nil {
		t.Fatal(err)
	}
	state, err = s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Runs[0].Status != "done" || state.Runs[0].Output != "虚构最终方案" || peak.Load() < 2 || !strings.Contains(state.Runs[0].Brief, "虚构正文必须保留") || !strings.Contains(state.Runs[0].Brief, "虚构交接讨论必须保留") {
		t.Fatal(state.Runs[0], peak.Load())
	}
	var calls int
	if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND tier='heavy'", scope.OwnerID).Scan(&calls); err != nil || calls != 8 {
		t.Fatal(calls, err)
	}
}

func TestB4RetiredAndStaleCardItemsStayOut(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var prompt string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		_, prompt = b4RequestBody(t, r)
		secretaryModelReply(w, `{"reply":"虚构回答","actions":[]}`)
	})
	old := b4Memory(t, s, scope, "虚构已退出的紫松计划")
	kept := b4Memory(t, s, scope, "虚构当前的紫松计划")
	b4Card(t, s, scope, "self:goal", "self", "紫松计划", old, kept)
	b4Exec(t, s, "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, old.ID, kept.ID)
	if _, err := s.DeskTurn(b4Context(s), scope, turnRequest("紫松计划现在怎样")); err != nil {
		t.Fatal(err)
	}
	memorySection := strings.Split(prompt, "相关原话")[0]
	if strings.Contains(memorySection, old.Text) || !strings.Contains(memorySection, kept.Text) {
		t.Fatal(prompt)
	}
}

func TestB4MediumTimeoutUsesDraftWithoutRetry(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	checkElapsed := make(chan time.Duration, 1)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(30 * time.Millisecond)
			secretaryModelReply(w, `{"reply":"虚构可用草稿","actions":[]}`)
			return
		}
		_, _ = b4RequestBody(t, r)
		started := time.Now()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		checkElapsed <- time.Since(started)
	})
	m := b4Memory(t, s, scope, "虚构可用卡片背景")
	b4Card(t, s, scope, "self:goal", "self", "虚构目标", m)
	out, err := s.DeskTurn(b4Context(s), scope, turnRequest("认真整理虚构资料"))
	if err != nil || out.Turn.Reply != "虚构可用草稿" || calls.Load() != 2 {
		t.Fatal(out, err, calls.Load())
	}
	select {
	case elapsed := <-checkElapsed:
		if elapsed > 150*time.Millisecond {
			t.Fatal("selfcheck ignored first-call budget", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("selfcheck did not cancel its HTTP request")
	}
	// Late accounting survives the completed turn's durable-context cancellation.
	deadline := time.Now().Add(time.Second)
	for {
		var n int
		if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND turn_id=$2 AND purpose='selfcheck' AND tier='medium'", scope.OwnerID, out.Turn.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed-out selfcheck lost its usage record")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestB4HeavyCutoffUsesFinishedReaders(t *testing.T) {
	s := testStore(t)
	scope := owner()
	keys := []string{}
	ids := []string{}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		_, prompt := b4RequestBody(t, r)
		if strings.Contains(prompt, "分组目录") {
			secretaryModelReply(w, map[string]any{"groups": keys})
			return
		}
		if strings.Contains(prompt, "虚构慢读者") {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		for _, id := range ids {
			if strings.Contains(prompt, id) {
				secretaryModelReply(w, map[string]any{"used": []string{id}})
				return
			}
		}
		t.Error("reader without IDs")
	})
	for i := 0; i < 3; i++ {
		text := fmt.Sprintf("虚构快读者%d", i)
		if i == 2 {
			text = "虚构慢读者"
		}
		m := b4Memory(t, s, scope, text)
		ids = append(ids, m.ID)
		var eid string
		err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
			id, e := entityTx(context.Background(), tx, scope.OwnerID, "topic", fmt.Sprintf("虚构限时组%d", i))
			eid = string(id)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, "entity:"+eid)
		b4Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,'topic')`, scope.OwnerID, m.ID, m.Version, eid)
	}
	var agent workspace.Agent
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		var err error
		agent, err = queryDocument[workspace.Agent](context.Background(), tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id='model'", scope.OwnerID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	u := useContext{Ready: true}
	for _, key := range keys {
		u.Index = append(u.Index, workspace.StatusCardRef{Key: key, Name: "虚构限时组", Count: 3})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	picked, _, chosen := s.heavyUse(ctx, context.Background(), scope, agent, nil, "虚构请求", u, "", "")
	if len(picked) != 2 || len(chosen) != 3 || time.Since(start) > 1500*time.Millisecond {
		t.Fatal(len(picked), chosen, time.Since(start))
	}
}

func TestB4RankFusionKeepsStructuredMatchesFirst(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"虚构回答","actions":[]}`)
	})
	exact := b4Memory(t, s, scope, "虚构人物的明确约束")
	vector := b4Memory(t, s, scope, "虚构璃风的另一段安排")
	var entity string
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		id, e := entityTx(context.Background(), tx, scope.OwnerID, "person", "虚构璃风")
		entity = string(id)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	b4Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,'person')`, scope.OwnerID, exact.ID, exact.Version, entity)
	for _, m := range []workspace.Memory{exact, vector} {
		value := "[0,1]"
		if m.ID == vector.ID {
			value = "[1,0]"
		}
		b4Exec(t, s, `INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,'b4-fictional-vector',2,$4::vector)`, scope.OwnerID, m.ID, m.Version, value)
	}
	var result memory.RecallResult
	err = pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		return s.recallTx(context.Background(), tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}, memory.RecallRequest{Team: &memory.TeamRecall{Text: "虚构璃风", RankFusion: true}, Mode: memory.Remember}, memory.Budget{Candidates: 40, Tokens: 12000}, "虚构璃风", "'虚构璃风'", []byte("[1,0]"), "b4-fictional-vector", 0, "", memory.SearchTokens("虚构璃风"), &result)
	})
	if err != nil || len(result.Memories) < 2 || string(result.Memories[0].ID) != exact.ID {
		t.Fatal(result, err)
	}
}

func TestB4RuleScopeAndSafePlans(t *testing.T) {
	if ruleRelevance("起草邮件", "帮我写一封邮件") == 0 || ruleRelevance("", "虚构杂事") == 0 || ruleRelevance("盆栽浇水", "起草邮件") != 0 {
		t.Fatal("rule applicability")
	}
	raw := safeUsePlan(json.RawMessage(`{"groups":["secret text","self:rule"],"question":"private text"}`))
	if strings.Contains(string(asJSON(raw)), "private") || strings.Contains(string(asJSON(raw)), "secret") {
		t.Fatal("usage plan leaked text")
	}
}

func TestB4BulkValidationRejectsChangesDuringGeneration(t *testing.T) {
	for _, mode := range []string{"correct", "grant", "exclude", "assistant_evidence"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			var target, item string
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "correct":
					workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: target, Text: "虚构已纠正的约束"})
				case "grant":
					workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: target, AgentIDs: []string{"manual"}})
				case "assistant_evidence":
					b4Exec(t, s, `INSERT INTO source_contexts(owner_id,source_id,source_version,role,branch) SELECT DISTINCT owner_id,source_id,source_version,'assistant','active' FROM evidence WHERE owner_id=$1 AND target_id=$2 AND stance='supports' ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET role='assistant'`, scope.OwnerID, target)
				case "exclude":
					workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: item, MemoryID: target})
				}
				secretaryModelReply(w, `{"reply":"虚构过时回答","actions":[{"op":"create_task","title":"虚构不该执行的新任务"}]}`)
			})
			ms := []workspace.Memory{}
			for i := 0; i < 3; i++ {
				ms = append(ms, b4Memory(t, s, scope, fmt.Sprintf("虚构卡约束背景%d", i)))
			}
			target = ms[0].ID
			b4Card(t, s, scope, "self:goal", "self", "虚构卡约束", ms...)
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构当前事项"})
			item = state.Tasks[0].ID
			req := turnRequest("按虚构卡约束建一个新待办")
			req.ThingID = &item
			out, err := s.DeskTurn(b4Context(s), scope, req)
			if err != nil || len(out.State.Tasks) != 1 || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" {
				t.Fatal(out, err)
			}
		})
	}
}

func TestB4UnbuiltStatusKeepsOriginalSingleCall(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"reply":"虚构旧路径答复","actions":[]}`)
	})
	out, err := s.DeskTurn(b4Context(s), scope, turnRequest("虚构事项现在如何"))
	if err != nil || calls.Load() != 1 || out.Turn.Reply != "虚构旧路径答复" {
		t.Fatal(out, err, calls.Load())
	}
}

func TestB4NamedGroupsLeadAndRespectCaps(t *testing.T) {
	index := []workspace.StatusCardRef{}
	ranked := []workspace.Memory{}
	for i := 0; i < 10; i++ {
		id := string(memory.NewID())
		index = append(index, workspace.StatusCardRef{Key: "entity:" + id, Name: fmt.Sprintf("虚构组%d", i)})
		ranked = append(ranked, workspace.Memory{Groups: []workspace.MemoryGroup{{EntityID: id}}})
	}
	aliases := map[string][]string{index[9].Key: {"虚构第九别名"}}
	selected := chooseUseGroups("虚构组8和虚构第九别名", index, ranked, aliases, 6, false)
	if len(selected) != 6 || selected[0] != index[8].Key || selected[1] != index[9].Key {
		t.Fatal(selected)
	}
	selected = chooseUseGroups("虚构组0 虚构组1", index, ranked, aliases, 6, false)
	if len(selected) != 6 || selected[0] != index[0].Key || selected[1] != index[1].Key {
		t.Fatal(selected)
	}
	selected = chooseUseGroups("虚构组0 虚构组1 虚构组2 虚构组3 虚构组4 虚构组5 虚构组6 虚构组7 虚构组8 虚构组9", index, ranked, aliases, 6, false)
	for i, key := range selected {
		if key != index[i].Key {
			t.Fatal(selected)
		}
	}
	if len(selected) != 6 {
		t.Fatal(selected)
	}
	selected = chooseUseGroups("虚构组0 虚构组1 虚构组2 虚构组3 虚构组4 虚构组5 虚构组6 虚构组7 虚构组8 虚构组9", index, ranked, aliases, 12, true)
	if len(selected) != 10 {
		t.Fatal(selected)
	}
}

func TestB4ProductionCardsUseAppliesToAndNamedOrder(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var prompt string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		_, prompt = b4RequestBody(t, r)
		secretaryModelReply(w, `{"reply":"虚构联调成功","actions":[]}`)
	})
	for _, name := range []string{"阿岚主题", "紫霁主题"} {
		var id memory.ID
		if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
			var e error
			id, e = entityTx(context.Background(), tx, scope.OwnerID, "topic", name)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		m := b4Memory(t, s, scope, "虚构龙纹季度进展记录："+name)
		b4Card(t, s, scope, "entity:"+string(id), "topic", name, m)
	}
	email := b4Memory(t, s, scope, "虚构要求：发出去之前先给我看")
	email.AppliesTo = "起草邮件"
	expense := b4Memory(t, s, scope, "虚构报销专用要求")
	expense.AppliesTo = "财务报销"
	global := b4Memory(t, s, scope, "虚构不限范围要求")
	b4Card(t, s, scope, "self:rule", "self", "对助手的要求", email, expense, global)
	out, err := s.DeskTurn(context.Background(), scope, turnRequest("按龙纹季度进展起草邮件，紫霁主题先办"))
	if err != nil || out.Turn.Reply != "虚构联调成功" {
		t.Fatal(out, err)
	}
	if !strings.Contains(prompt, email.Text) || !strings.Contains(prompt, global.Text) {
		t.Fatal(prompt)
	}
	requirements := strings.Split(strings.Split(prompt, "必须遵守的要求")[1], "相关的现状卡")[0]
	if strings.Contains(requirements, expense.Text) {
		t.Fatal("unrelated appliesTo requirement", requirements)
	}
	first, second := strings.Index(prompt, "〔topic·紫霁主题〕"), strings.Index(prompt, "〔topic·阿岚主题〕")
	if first < 0 || second < 0 || first > second {
		t.Fatal("named card must lead", prompt)
	}
}
