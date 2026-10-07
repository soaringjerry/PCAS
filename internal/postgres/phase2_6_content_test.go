package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Library/foreground preconditions are authored from the frozen gold, not from
// the processor's output. This does NOT substitute for the B2 backfill test.
func phase26LibraryPrecondition(t *testing.T, f *phase26LoadedFixture) {
	t.Helper()
	err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, r := range f.Corpus.Rules {
			batch.Queue(`INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) VALUES($1,$2,1,$3,$4)`, f.Scope.OwnerID, f.Claims[r.Memory], r.Scope == "", r.Scope)
		}
		for _, d := range f.Corpus.Deadlines {
			kind := d.Kind
			if d.State == "unclear" {
				kind = "unclear"
			}
			batch.Queue(`INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text) VALUES($1,$2,$3,1,$4,$5,$6,$7,$8,$9)`, f.Scope.OwnerID, memory.NewID(), f.Claims[d.Memory], kind, d.At, d.Recurrence, fmt.Sprintf("Fictitious deadline %d", d.Memory), "Fictitious date clarification", d.Original)
		}
		return tx.SendBatch(f.Context, batch).Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}
func phase26Digest(t *testing.T, f *phase26LoadedFixture, tables []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables {
		var hash string
		sql := fmt.Sprintf(`SELECT md5(coalesce(string_agg(to_jsonb(t)::text||t.xmin::text||t.ctid::text,'|' ORDER BY to_jsonb(t)::text),'empty')) FROM %s t WHERE owner_id=$1`, table)
		if err := f.Store.pool.QueryRow(f.Context, sql, f.Scope.OwnerID).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		out[table] = hash
	}
	return out
}
func TestPhase26G6B3B7B8LibraryAndC1C2Content(t *testing.T) {
	f := phase26LoadFixture(t)
	model := phase26NewModel(t, f)
	phase26LibraryPrecondition(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	s, ctx := f.Store, f.Context
	phase26Exec(t, f, `INSERT INTO handovers(owner_id,body,rule,built_at,stale,depends,input_hash) VALUES($1,'Fictitious old handover',2,now()-interval '2 hours',true,'[]','fictitious old hash')`, f.Scope.OwnerID)
	if _, err := s.Snapshot(ctx, f.Scope); err != nil {
		t.Fatal(err)
	}
	tables := []string{"claims", "claim_revisions", "claim_mentions", "entity_versions", "aliases", "record_grants", "evidence", "deadlines", "assistant_requirements", "handovers", "status_cards", "status_card_items", "workspace_owners"}
	before := phase26Digest(t, f, tables)
	dates, err := s.ListDeadlines(ctx, f.Scope, workspace.DeadlineQuery{})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]int{}
	for _, d := range dates.Items {
		states[d.DateStatus]++
	}
	if !reflect.DeepEqual(states, map[string]int{"upcoming": 12, "expired_unknown": 12, "recurring": 12, "unclear": 12}) {
		t.Errorf("deadline state counts=%v", states)
	}
	rules, err := s.ListAssistantRequirements(ctx, f.Scope)
	if err != nil || len(rules.Items) != 366 {
		t.Fatalf("requirement rows=%d err=%v", len(rules.Items), err)
	}
	groups, err := s.ListMemoryGroups(ctx, f.Scope)
	if err != nil || len(groups.Items) != 300 {
		t.Fatalf("directory groups=%d err=%v", len(groups.Items), err)
	}
	for g, gold := range f.Corpus.Groups {
		key := "self:" + gold.Kind
		if gold.Entity >= 0 {
			key = "entity:" + string(f.Entities[g])
		} else {
			key = map[int]string{296: "self:rule", 297: "self:identity", 298: "self:taste", 299: "self:goal"}[g]
		}
		page, err := s.ListGroupMemories(ctx, f.Scope, key, workspace.GroupMemoryQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != len(gold.Members) {
			t.Errorf("group %d total=%d gold=%d", g, page.Total, len(gold.Members))
		}
	}
	old, err := s.ReadHandover(ctx, f.Scope)
	if err != nil || old.Body != "Fictitious old handover" || !old.Stale {
		t.Fatalf("stale handover unavailable: %+v %v", old, err)
	}
	if !reflect.DeepEqual(before, phase26Digest(t, f, tables)) {
		t.Error("read changed business content/tuple/owner version")
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent := workspace.Agent{ID: "phase26", MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}}
		u, err := s.startUseContextTx(ctx, tx, f.Scope)
		if err != nil {
			return err
		}
		if err = s.finishUseContextTx(ctx, tx, f.Scope, agent, nil, "A fictitious question with no scope keywords", nil, &u); err != nil {
			return err
		}
		unrestricted := 0
		scopeByID := map[string]string{}
		for _, r := range f.Corpus.Rules {
			scopeByID[string(f.Claims[r.Memory])] = r.Scope
		}
		for _, r := range u.Rules {
			scope, ok := scopeByID[r.ID]
			if !ok {
				t.Errorf("unknown requirement %s", r.ID)
			}
			if scope == "" {
				unrestricted++
			} else if !strings.Contains(u.RequirementScopes[r.ID], scope) {
				t.Error("lost requirement scope")
			}
		}
		if len(u.Rules) != 366 || unrestricted != 26 || len(u.Deadlines) != 48 {
			t.Errorf("per-turn content rules=%d unrestricted=%d deadlines=%d", len(u.Rules), unrestricted, len(u.Deadlines))
		}
		var b strings.Builder
		writeUseContext(&b, u, time.UTC, func(m workspace.Memory) { b.WriteString(m.Text); b.WriteByte('\n') })
		prompt := b.String()
		if !strings.Contains(prompt, old.Body) || !strings.Contains(prompt, old.BuiltAt) || !strings.Contains(prompt, "过期=true") {
			t.Error("stale handover body/timestamp/staleness absent from every-turn context")
		}
		t.Logf("per-turn rules=%d unrestricted=%d deadlines=%d old_handover=%s", len(u.Rules), unrestricted, len(u.Deadlines), old.BuiltAt)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, HandoverStage)
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, HandoverStage)
	j := phase26ClaimStage(t, f, HandoverStage)
	if err := s.ProcessHandover(ctx, j); err != nil {
		t.Fatal(err)
	}
	calls := model.calls(HandoverStage)
	if len(calls) != 1 {
		t.Fatal("handover made extra provider calls")
	}
	var input struct {
		Refs         []memory.Ref
		Requirements []workspace.AssistantRequirement
		Deadlines    []workspace.LibraryDeadline
	}
	if err := json.Unmarshal([]byte(calls[0].Prompt), &input); err != nil {
		t.Fatal(err)
	}
	expected := map[memory.ID]bool{}
	for _, indices := range f.Corpus.Handover {
		for _, i := range indices {
			expected[f.Claims[i]] = true
		}
	}
	actual := map[memory.ID]bool{}
	for _, ref := range input.Refs {
		actual[ref.ID] = true
	}
	if !reflect.DeepEqual(expected, actual) || len(input.Requirements) != 366 || len(input.Deadlines) != 48 {
		t.Errorf("handover content refs actual=%d gold=%d requirements=%d deadlines=%d", len(actual), len(expected), len(input.Requirements), len(input.Deadlines))
	}
	var output struct{ Sections []struct{ Refs []int } }
	if err := json.Unmarshal([]byte(calls[0].Output), &output); err != nil {
		t.Fatal(err)
	}
	used := map[memory.ID]bool{}
	for _, section := range output.Sections {
		for _, n := range section.Refs {
			if n < 1 || n > len(input.Refs) {
				t.Fatal("handover invented source reference")
			}
			used[input.Refs[n-1].ID] = true
		}
	}
	if !reflect.DeepEqual(expected, used) {
		t.Errorf("handover output source coverage=%d expected=%d", len(used), len(expected))
	}
	t.Logf("actual handover input refs=%d requirements=%d deadlines=%d; all gold identities match", len(actual), len(input.Requirements), len(input.Deadlines))
	phase26Exec(t, f, `UPDATE claim_revisions SET value=to_jsonb('Fictitious unrelated event edit'::text) WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, f.Claims[4999])
	if n, err := s.ScheduleStatus(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("unrelated event rebuilt handover queued=%d err=%v", n, err)
	}
	good, err := s.ReadHandover(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	phase26Exec(t, f, `UPDATE claim_revisions SET value=to_jsonb('Fictitious changed requirement'::text) WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, f.Claims[2])
	phase26Isolate(t, f, HandoverStage)
	if n, err := s.ScheduleStatus(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("changed requirement not queued for hourly update: %d %v", n, err)
	}
	var delay float64
	if err := s.pool.QueryRow(ctx, `SELECT extract(epoch FROM available_at-now())::float8 FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.handover:%' AND state='queued'`, f.Scope.OwnerID).Scan(&delay); err != nil || delay < 3590 || delay > 3610 {
		t.Fatalf("hourly debounce seconds=%f err=%v", delay, err)
	}
	stale, err := s.ReadHandover(ctx, f.Scope)
	if err != nil || stale.Body != good.Body || stale.BuiltAt != good.BuiltAt || !stale.Stale {
		t.Errorf("hourly wait lost previous handover: %+v %v", stale, err)
	}
}

func TestPhase26G4A6A10ComparisonCoverageAndUnchangedScheduling(t *testing.T) {
	f := phase26LoadFixture(t)
	phase26NewModel(t, f)
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	s, ctx := f.Store, f.Context
	var first, second []comparisonBatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := syncComparisonMembersTx(ctx, tx, f.Scope.OwnerID); err != nil {
			return err
		}
		var err error
		first, err = comparisonPlanTx(ctx, tx, f.Scope.OwnerID, CompareVersion)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || first[0].Group.Key != "self:rule" {
		t.Fatal("requirements not first")
	}
	pairs := map[[2]memory.ID]bool{}
	largest := "entity:" + string(f.Entities[0])
	largestCalls := 0
	for _, b := range first {
		if len(b.Refs) > 200 {
			t.Error("unbatched comparison")
		}
		if b.Group.Key == largest {
			largestCalls++
		}
		for _, p := range f.Corpus.Pairs {
			if b.Group.Key != map[bool]string{true: "self:rule", false: "entity:" + string(f.Entities[p.Group])}[p.Group == 296] {
				continue
			}
			seen := map[memory.ID]bool{}
			for _, r := range b.Refs {
				seen[r.ID] = true
			}
			if seen[f.Claims[p.Early]] && seen[f.Claims[p.Recent]] {
				pairs[[2]memory.ID{f.Claims[p.Early], f.Claims[p.Recent]}] = true
			}
		}
	}
	if largestCalls != 78 || len(pairs) != 3 {
		t.Fatalf("1200-member opportunity coverage calls=%d expected78 goldpairs=%d expected3", largestCalls, len(pairs))
	}
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCandidatesStage, HandoverStage} {
		if _, err := phase26Schedule(ctx, s, stage); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"memory_comparison_members", "memory_group_progress", "memory_jobs", "claims", "entity_versions"}
	before := phase26Digest(t, f, tables)
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCandidatesStage, HandoverStage} {
		if _, err := phase26Schedule(ctx, s, stage); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, phase26Digest(t, f, tables)) {
		t.Error("unchanged scheduling rewrote tuples")
	}
	// One claim edit is a real content change; compare fingerprints must change
	// exactly for batches containing that claim, not all 300 groups.
	changed := f.Claims[148]
	phase26Exec(t, f, `UPDATE claim_revisions SET value=to_jsonb('Fictitious edited note'::text) WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, changed)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		second, err = comparisonPlanTx(ctx, tx, f.Scope.OwnerID, CompareVersion)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatal("unrelated plan topology changed")
	}
	affected := 0
	for i, b := range first {
		has := false
		for _, r := range b.Refs {
			has = has || r.ID == changed
		}
		if (b.Fingerprint != second[i].Fingerprint) != has {
			t.Errorf("unrelated comparison batch changed: %s/%d/%d", b.Group.Key, b.A, b.B)
		}
		if has {
			affected++
		}
	}
	t.Logf("complete plan batches=%d largest_group_calls=%d single_memory_affected_calls=%d; plan coverage is not actual provider billing", len(first), largestCalls, affected)
	phase26LibraryPrecondition(t, f)
	phase26Isolate(t, f, CompareStage)
	for i := 0; i < 4; i++ {
		if _, err := s.ScheduleCompare(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, CompareStage)
		j := phase26ClaimStage(t, f, CompareStage)
		if err := s.ProcessCompare(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := s.ListAssistantRequirements(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.Items) != 363 {
		t.Errorf("gold deduplicated/corrected requirements=%d expected363", len(rules.Items))
	}
	surviving := map[string]bool{}
	unrestricted := 0
	for _, r := range rules.Items {
		surviving[r.MemoryID] = true
		if r.Unrestricted {
			unrestricted++
		}
	}
	if unrestricted != 24 {
		t.Errorf("effective unrestricted rules=%d expected24", unrestricted)
	}
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := s.Snapshot(ctx, f.Scope); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := s.startUseContextTx(ctx, tx, f.Scope)
		if err != nil {
			return err
		}
		if err := s.finishUseContextTx(ctx, tx, f.Scope, workspace.Agent{ID: "phase26", MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}}, nil, "Fictitious request after requirement deduplication", nil, &u); err != nil {
			return err
		}
		unrestricted := 0
		for _, r := range u.Rules {
			if strings.HasPrefix(u.RequirementScopes[r.ID], "不限范围") {
				unrestricted++
			}
		}
		if len(u.Rules) != 363 || unrestricted != 24 {
			t.Errorf("effective per-turn rules=%d unrestricted=%d expected363/24", len(u.Rules), unrestricted)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Corpus.Pairs {
		if surviving[string(f.Claims[p.Early])] || !surviving[string(f.Claims[p.Recent])] {
			t.Errorf("gold comparison relation not applied: %+v", p)
		}
	}
}
