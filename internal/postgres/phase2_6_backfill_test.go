package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26T3T4ScaleBackfillAndReadOnlyContent(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	s, ctx, owner := f.Store, f.Context, f.Scope.OwnerID
	phase26Isolate(t, f, OrganizeStage)
	start := time.Now()
	windows, deferrals := 1, 0
	for completed := 0; completed < 125; {
		if _, err := s.ScheduleOrganize(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		j := phase26ClaimStage(t, f, OrganizeStage)
		err := s.ProcessOrganize(ctx, j)
		if err != nil {
			t.Logf("backfill diagnostic: successful=%d actual_provider_calls=%d process_error=%T %v context=%v", completed, len(m.calls(OrganizeStage)), err, err, ctx.Err())
			e := phase26FinishError(t, f, j, err)
			if e.Code != "organize_hourly_limit" || !e.NoAttempt || completed != 40*windows {
				t.Fatalf("unexpected backfill deferral at call %d: %+v", completed, e)
			}
			deferrals++
			windows++
			// Simulate a rolling-hour release between fully joined processing calls.
			// Retain every billing/queue row; do not claim real 3.125-hour elapsed time.
			phase26Exec(t, f, `UPDATE background_usage SET created_at=created_at-interval '61 minutes' WHERE stage='memory.organize'`)
			phase26Exec(t, f, `UPDATE memory_jobs SET available_at=now() WHERE id=$1`, j.ID)
			continue
		}
		completed++
	}
	if len(m.calls(OrganizeStage)) != 125 || windows != 4 || deferrals != 3 {
		t.Fatalf("calls=%d windows=%d deferrals=%d", len(m.calls(OrganizeStage)), windows, deferrals)
	}
	var marked, usage, done int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND organized>=2 AND retired=''`, owner).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_usage WHERE owner_id=$1 AND stage='memory.organize'`, owner).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='done'`, owner).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if marked != 5000 || usage != 125 || done != 125 {
		t.Fatalf("marked=%d usage=%d done=%d", marked, usage, done)
	}
	dates, err := s.ListDeadlines(ctx, f.Scope, workspace.DeadlineQuery{})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]phase26DeadlineGold{}
	for _, d := range f.Corpus.Deadlines {
		expected[string(f.Claims[d.Memory])] = d
	}
	if len(dates.Items) != 48 {
		t.Fatalf("planted=48 extracted=%d", len(dates.Items))
	}
	states := map[string]int{}
	for _, d := range dates.Items {
		gold, ok := expected[d.MemoryID]
		if !ok {
			t.Fatal("unplanted deadline returned")
		}
		delete(expected, d.MemoryID)
		wantKind, wantState := gold.Kind, map[string]string{"future": "upcoming", "expired": "expired_unknown", "recurring": "recurring", "unclear": "unclear"}[gold.State]
		if gold.State == "unclear" {
			wantKind = "unclear"
		}
		if d.Kind != wantKind || d.DateStatus != wantState || d.OriginalText != gold.Original || d.Recurrence != gold.Recurrence {
			t.Fatalf("deadline content differs: memory=%d got=%+v want=%+v", gold.Memory, d, gold)
		}
		if gold.At == nil {
			if d.At != nil {
				t.Fatal("invented date")
			}
		} else if d.At == nil || *d.At != gold.At.Format(time.RFC3339) {
			t.Fatal("wrong model date")
		}
		states[d.DateStatus]++
	}
	if len(expected) != 0 {
		t.Fatal("lost deadline sources")
	}
	yes, no := true, false
	for _, filter := range []*bool{&yes, &no} {
		list, err := s.ListDeadlines(ctx, f.Scope, workspace.DeadlineQuery{Expired: filter})
		if err != nil {
			t.Fatal(err)
		}
		want := 36
		if *filter {
			want = 12
		}
		if len(list.Items) != want {
			t.Fatalf("expired=%t count=%d", *filter, len(list.Items))
		}
	}
	rules, err := s.ListAssistantRequirements(ctx, f.Scope)
	if err != nil || len(rules.Items) != 366 {
		t.Fatalf("requirements before comparison=%d error=%v", len(rules.Items), err)
	}
	goldRules := map[string]phase26RuleGold{}
	for _, r := range f.Corpus.Rules {
		goldRules[string(f.Claims[r.Memory])] = r
	}
	for _, r := range rules.Items {
		g, ok := goldRules[r.MemoryID]
		if !ok || r.Unrestricted != (g.Scope == "") || r.Scope != g.Scope || r.Text != f.Corpus.Memories[g.Memory].Text {
			t.Fatalf("requirement scope/content differs: %+v", r)
		}
	}
	// Read-only means unchanged business rows AND unchanged tuples, not just equal
	// returned content. Include stale handover/card tables and owner versions.
	digest := func() map[string]string {
		out := map[string]string{}
		for _, table := range []string{"claims", "claim_revisions", "claim_mentions", "entity_versions", "aliases", "record_grants", "evidence", "deadlines", "assistant_requirements", "handovers", "status_cards", "status_card_items", "workspace_owners"} {
			var hash string
			sql := fmt.Sprintf(`SELECT md5(coalesce(string_agg(to_jsonb(t)::text||t.xmin::text||t.ctid::text,'|' ORDER BY to_jsonb(t)::text),'empty')) FROM %s t WHERE owner_id=$1`, table)
			if err := s.pool.QueryRow(ctx, sql, owner).Scan(&hash); err != nil {
				t.Fatal(err)
			}
			out[table] = hash
		}
		return out
	}
	before := digest()
	if _, err := s.ReadHandover(ctx, f.Scope); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListMemoryGroups(ctx, f.Scope); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"self:rule", "entity:" + string(f.Entities[0])} {
		if _, err := s.ListGroupMemories(ctx, f.Scope, g, workspace.GroupMemoryQuery{Limit: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ListDeadlines(ctx, f.Scope, workspace.DeadlineQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListAssistantRequirements(ctx, f.Scope); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, digest()) {
		t.Fatal("library read mutated data or owner/tuple versions")
	}
	health, err := s.BackgroundHealth(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var h any
	if err := json.Unmarshal(health, &h); err != nil {
		t.Fatal(err)
	}
	t.Logf("backfill successful_calls=%d marked=%d retained_usage=%d quota_windows=%d hourly_deferrals=%d actual_elapsed=%s deadline_states=%v health=%s", len(m.calls(OrganizeStage)), marked, usage, windows, deferrals, time.Since(start), states, health)
}
