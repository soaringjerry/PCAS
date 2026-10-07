package postgres

import (
	"context"
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/memory"
	"net/http"
	"testing"
	"time"
)

func TestP3ReorganizeTenCasesOnlyRulesAndWithdrawalCounts(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	expected := map[string]string{}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		var input struct {
			Memories []organizeMemory `json:"memories"`
		}
		if e := json.Unmarshal([]byte(req.Messages[1].Content), &input); e != nil {
			t.Error(e)
			return
		}
		items := []any{}
		for _, m := range input.Memories {
			category := expected[m.Text]
			if category == "" {
				t.Error("reorganized unrelated category", m.Text)
			}
			dates := []any{}
			if category == "event" && m.Text != p3RuleCases[8].Text {
				dates = append(dates, map[string]any{"kind": "deadline", "at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), "title": "虚构真实一次性期限", "recurrence": "", "timeNote": ""})
			}
			items = append(items, map[string]any{"n": m.N, "category": category, "durable": category == "rule", "unrestricted": true, "scope": "", "deadlines": dates})
		}
		raw, _ := json.Marshal(map[string]any{"items": items, "new": []any{}})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}})
	})
	refs := []memory.Ref{}
	for i, c := range p3RuleCases {
		expected[c.Text] = c.Category
		ref := organizeTestMemory(t, s, scope, c.Text)
		refs = append(refs, ref)
		if _, e := s.pool.Exec(ctx, `UPDATE claims SET organized=2 WHERE id=$1;
 `, ref.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, "UPDATE claim_revisions SET category='rule' WHERE claim_id=$1", ref.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, `INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) VALUES($1,$2,1,true,'');
 `, scope.OwnerID, ref.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,title) VALUES($1,$2,$3,1,'unclear','虚构误抽期限')`, scope.OwnerID, memory.NewID(), ref.ID); e != nil {
			t.Fatal(e)
		}
		// Retired rule memories are also targeted, without changing their retirement.
		if i == 8 {
			if _, e := s.pool.Exec(ctx, "UPDATE claims SET retired='duplicate',retired_by=$2 WHERE id=$1", ref.ID, refs[0].ID); e != nil {
				t.Fatal(e)
			}
		}
	}
	other := organizeTestMemory(t, s, scope, "虚构本人喜欢绿色纸张。")
	if _, e := s.pool.Exec(ctx, "UPDATE claim_revisions SET category='taste',durable=true WHERE claim_id=$1", other.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE claims SET organized=2 WHERE id=$1", other.ID); e != nil {
		t.Fatal(e)
	}
	if n, e := s.QueueRuleReorganize(ctx, scope); e != nil || n != 10 {
		t.Fatal(n, e)
	}
	job := organizeTestJob(t, s, scope)
	if e := s.ProcessOrganize(ctx, job); e != nil {
		t.Fatal(e)
	}
	for i, ref := range refs {
		var category string
		var organized int
		if e := s.pool.QueryRow(ctx, "SELECT c.category,cl.organized FROM claims cl JOIN claim_revisions c ON c.claim_id=cl.id WHERE cl.id=$1", ref.ID).Scan(&category, &organized); e != nil || category != p3RuleCases[i].Category || organized != 3 {
			t.Fatal(i, category, organized, e)
		}
	}
	var marker int
	var category string
	if e := s.pool.QueryRow(ctx, "SELECT cl.organized,c.category FROM claims cl JOIN claim_revisions c ON c.claim_id=cl.id WHERE cl.id=$1", other.ID).Scan(&marker, &category); e != nil || marker != 2 || category != "taste" {
		t.Fatal("changed non-rule", marker, category, e)
	}
	var dates, requirements int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM deadlines),(SELECT count(*) FROM assistant_requirements)").Scan(&dates, &requirements); e != nil || dates != 4 || requirements != 5 {
		t.Fatal(dates, requirements, e)
	}
	var withdrawnDates, withdrawnRequirements int
	if e := s.pool.QueryRow(ctx, `SELECT coalesce(sum(count) FILTER(WHERE reason='reorganize_deadlines_withdrawn'),0),coalesce(sum(count) FILTER(WHERE reason='reorganize_requirements_withdrawn'),0) FROM background_stage_events`).Scan(&withdrawnDates, &withdrawnRequirements); e != nil || withdrawnDates != 10 || withdrawnRequirements != 5 {
		t.Fatal(withdrawnDates, withdrawnRequirements, e)
	}
	health, e := s.BackgroundHealth(ctx, scope)
	if e != nil {
		t.Fatal(e)
	}
	var h struct {
		Stages []struct {
			Stage     string                                `json:"stage"`
			Withdrawn struct{ Deadlines, Requirements int } `json:"withdrawn"`
		} `json:"stages"`
	}
	if e = json.Unmarshal(health, &h); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, stage := range h.Stages {
		if stage.Stage == OrganizeStage {
			found = stage.Withdrawn.Deadlines == 10 && stage.Withdrawn.Requirements == 5
		}
	}
	if !found {
		t.Fatal("withdrawals not visible", string(health))
	}
	if n, e := s.QueueRuleReorganize(ctx, scope); e != nil || n != 0 {
		t.Fatal("reran completed rules", n, e)
	}
}
func TestP3RuleReorganizeFailureKeepsGoodLabelsAndTables(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	fake := b1Model(t, s, `{"items":[{"n":1,"category":"event","durable":false}],"new":[]}`) // missing required deadline array
	ref := organizeTestMemory(t, s, scope, p3RuleCases[5].Text)
	if _, e := s.pool.Exec(ctx, "UPDATE claim_revisions SET category='rule',durable=true WHERE claim_id=$1", ref.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE claims SET organized=2 WHERE id=$1", ref.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) VALUES($1,$2,1,true,'')", scope.OwnerID, ref.ID); e != nil {
		t.Fatal(e)
	}
	job := organizeTestJob(t, s, scope)
	if e := s.ProcessOrganize(ctx, job); e != nil {
		t.Fatal(e)
	}
	var category string
	var marker, reqs, attempts int
	if e := s.pool.QueryRow(ctx, "SELECT c.category,cl.organized,cl.organize_attempts,(SELECT count(*) FROM assistant_requirements) FROM claims cl JOIN claim_revisions c ON c.claim_id=cl.id WHERE cl.id=$1", ref.ID).Scan(&category, &marker, &attempts, &reqs); e != nil || category != "rule" || marker != 2 || attempts != 1 || reqs != 1 {
		t.Fatal(category, marker, attempts, reqs, e)
	}
	if len(fake.all()) != 1 {
		t.Fatal("calls", len(fake.all()))
	}
}
