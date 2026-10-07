package postgres

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26B6ModelAddsOneCanonicalNovelDomain(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	name := "Fictitious novel glaze research"
	m.mu.Lock()
	m.Reply = func(c phase26Call) string {
		if c.Stage != OrganizeStage {
			return m.goldReply(c)
		}
		var p struct{ Memories []struct{ N int } }
		_ = json.Unmarshal([]byte(c.Prompt), &p)
		items := []any{}
		for _, item := range p.Memories {
			items = append(items, map[string]any{"n": item.N, "category": "unknown", "durable": true, "area": name, "project": "", "topics": []string{}, "deadlines": []any{}, "unrestricted": false, "scope": ""})
		}
		b, _ := json.Marshal(map[string]any{"items": items, "new": []any{map[string]string{"type": "area", "name": name, "desc": "Fictitious canonical glaze research domain"}}})
		return string(b)
	}
	m.mu.Unlock()
	phase26Isolate(t, f, OrganizeStage)
	for i := 0; i < 2; i++ {
		if _, err := f.Store.ScheduleOrganize(f.Context, time.Now()); err != nil {
			t.Fatal(err)
		}
		j := phase26ClaimStage(t, f, OrganizeStage)
		if err := phase26Process(f.Context, f.Store, j); err != nil {
			t.Fatal(err)
		}
	}
	var domains, members int
	var id memory.ID
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*),min(entity_id::text) FROM entity_versions WHERE owner_id=$1 AND entity_type='area' AND name=$2`, f.Scope.OwnerID, name).Scan(&domains, &id); err != nil || domains != 1 {
		t.Fatalf("canonical novel domains=%d err=%v", domains, err)
	}
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, id).Scan(&members); err != nil || members != 80 {
		t.Fatalf("novel area members=%d err=%v", members, err)
	}
	if len(m.calls(OrganizeStage)) != 2 {
		t.Error("novel-domain validation made an extra model call")
	}
	t.Log("two 40-memory batches reuse one model-selected canonical novel domain, with no extra semantic model call")
}

func TestPhase26C3SemanticGroupAndDepthPiggybackPrimaryAnswer(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	phase26Exec(t, f, `INSERT INTO handovers(owner_id,body,rule,built_at,stale,depends,input_hash) VALUES($1,'Fictitious existing handover',2,now()-interval '2 hours',true,'[]','fictitious previous')`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	target := f.Corpus.Groups[295].Name
	line := regexp.MustCompile(`G[0-9]+：` + regexp.QuoteMeta(target) + `（`)
	ids := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	m.mu.Lock()
	m.Reply = func(c phase26Call) string {
		if strings.Contains(c.System, "你是用户的前台秘书") {
			alias := strings.TrimSuffix(strings.TrimSuffix(line.FindString(c.Prompt), "（"), "："+target)
			b, _ := json.Marshal(map[string]any{"reply": "Fictitious semantic answer.", "actions": []any{}, "used": []string{}, "memoryPlan": map[string]any{"depth": "heavy", "groups": []string{alias}}})
			return string(b)
		}
		if strings.Contains(c.Prompt, "\n分组：") {
			b, _ := json.Marshal(map[string]any{"used": ids.FindAllString(c.Prompt, -1)})
			return string(b)
		}
		return `{"reply":"Fictitious semantic answer.","actions":[],"used":[]}`
	}
	m.mu.Unlock()
	// Neither literal group name nor the old Chinese depth keywords occur here.
	req := workspace.DeskTurnRequest{AgentID: "phase26", RequestID: string(memory.NewID()), Text: "Please examine the overlooked fictitious shelf more carefully."}
	start := time.Now()
	out, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, req)
	if err != nil {
		t.Fatal(err)
	}
	primary, reader, selector := 0, 0, 0
	key := "entity:" + string(f.Entities[295])
	for _, c := range m.calls("secretary") {
		if strings.Contains(c.System, "你是用户的前台秘书") {
			primary++
		}
		if strings.Contains(c.Prompt, "\n分组："+key) {
			reader++
		}
		if strings.Contains(c.Prompt, "只选这些 key") {
			selector++
		}
	}
	// Coordinator change after the live rollout: scoped requirements are no
	// longer pre-sent, so the group reader can bring in material the first
	// answer had not seen and the turn answers once more with it. What this
	// case guards is that choosing the group and depth costs no selector call.
	if primary < 1 || primary > 2 || reader != 1 || selector != 0 || out.Turn.Reply != "Fictitious semantic answer." {
		t.Errorf("piggyback semantics primary=%d target_reader=%d extra_selector=%d reply=%q", primary, reader, selector, out.Turn.Reply)
	}
	t.Logf("semantic group/depth in primary answer: primary=%d reader=%d selector=%d elapsed=%s", primary, reader, selector, time.Since(start))
}
