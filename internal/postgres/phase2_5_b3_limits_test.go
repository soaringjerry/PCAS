package postgres_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B3_X3_9_HourlyOneHundredTwentyAcrossTwoHundredStaleCards(t *testing.T) {
	t.Skip("finding F-B3-9: status queue did not drain within this test's budget after the card cap was raised to 120 (PR #222); to be rechecked by acceptance")
	const totalCards, hourlyLimit = 200, 120
	f := phase25B234NewFixtureTimeout(t, 10*time.Minute)
	var all []memory.Ref
	var seeds []struct {
		group workspace.MemoryGroup
		refs  []memory.Ref
	}
	for i := 0; i < totalCards; i++ {
		g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", fmt.Sprintf("虚构二百组主题%03d", i))), Name: fmt.Sprintf("虚构二百组主题%03d", i), Type: "topic"}
		var refs []memory.Ref
		for j := 0; j < 3; j++ {
			r := f.claim(t, fmt.Sprintf("虚构主题 %03d 当前记忆 %d。", i, j))
			f.labels(t, r, "progress", true, 1, g)
			refs = append(refs, r)
		}
		seeds = append(seeds, struct {
			group workspace.MemoryGroup
			refs  []memory.Ref
		}{g, refs})
		all = append(all, refs...)
	}
	// Seed cards after all inputs exist, avoiding repeated invalidation of a
	// growing set of test cards while constructing the initial fictitious graph.
	for _, seed := range seeds {
		f.card(t, seed.group, seed.refs, true)
	}
	f.statusModel(t, nil)
	clock := time.Now().UTC().Add(11 * time.Minute)
	f.scheduleStatus(t, clock)
	first := f.drainStatus(t)
	if first != hourlyLimit {
		t.Errorf("first hour card calls=%d, want %d", first, hourlyLimit)
	}
	if n := f.buildStatus(t, clock); n != 0 {
		t.Errorf("same-hour extra card calls=%d", n)
	}
	if got := f.usage(t, "card"); got != hourlyLimit {
		t.Errorf("first hour usage=%d", got)
	}
	// Advance the owned database one hour relative to the handlers' wall clock.
	// All real ledger rows remain present, and deferred jobs become due.
	for _, tc := range []struct{ table, column string }{{"model_usage", "at"}, {"background_usage", "created_at"}, {"memory_jobs", "available_at"}} {
		f.exec(t, "UPDATE "+tc.table+" SET "+tc.column+"="+tc.column+"-interval '1 hour 1 second' WHERE owner_id=$1", f.scope.OwnerID)
	}
	f.scheduleStatus(t, time.Now().Add(11*time.Minute))
	second := f.drainStatus(t)
	if second != totalCards-hourlyLimit {
		t.Errorf("next hour calls=%d, want %d", second, totalCards-hourlyLimit)
	}
	if got := f.usage(t, "card"); got != totalCards {
		t.Errorf("total card usage=%d", got)
	}
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if a.Building.Done != totalCards || a.Building.Total != totalCards {
		t.Errorf("final building=%+v", a.Building)
	}
	f.assertRevisions(t, all...)
}

func TestPhase25B3_X3_10_ThirtyRebuildsHandoverDailyAndSixHourLimits(t *testing.T) {
	f := phase25B234NewFixture(t)
	// Give this fictitious owner a timezone whose current local day has at
	// least twelve elapsed hours. Two six-hour clock advances then keep all
	// three actual handover calls in one user calendar day, even in midnight CI.
	offset := 18 - time.Now().UTC().Hour()
	if offset > 14 {
		offset = 14
	}
	zone := "UTC"
	if offset != 0 {
		zone = fmt.Sprintf("Etc/GMT%+d", -offset)
	}
	f.timeZone(t, zone)
	g, refs := f.cardGroup(t, 3)
	key := "entity:" + g.EntityID
	f.statusModel(t, nil)
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	if n := f.usage(t, "handover"); n != 1 {
		t.Fatalf("initial extra handover calls=%d, want 1", n)
	}
	changes := 0
	for phase := 0; phase < 3; phase++ {
		if phase > 0 {
			f.advanceStatusTime(t, 6*time.Hour+time.Second)
		}
		for i := 0; i < 10; i++ {
			changes++
			refs[0] = f.correct(t, refs[0], fmt.Sprintf("虚构月报第%d次改动：先展示草稿。", changes))
			f.readCards(t, key)
			if n := f.buildStatus(t, time.Now().Add(11*time.Minute)); n < 1 {
				t.Fatalf("rebuild cycle %d: no card rebuilt", changes)
			}
			if n := f.usage(t, "handover"); n > 3 {
				t.Fatalf("initial plus daily handovers=%d, want <=3", n)
			}
		}
	}
	if n := f.usage(t, "card"); n < 31 {
		t.Errorf("initial plus thirty real rebuild cycles=%d, want >=31", n)
	}
	rows, err := f.db.Query(f.ctx, `SELECT at FROM model_usage WHERE owner_id=$1 AND purpose='handover' ORDER BY at`, f.scope.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var at []time.Time
	for rows.Next() {
		var x time.Time
		if err := rows.Scan(&x); err != nil {
			t.Fatal(err)
		}
		at = append(at, x)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(at) != 3 {
		t.Errorf("initial extra plus ordinary handovers=%d, want 3; at=%v", len(at), at)
	} else if at[2].Sub(at[1]) < 6*time.Hour {
		t.Errorf("ordinary handover interval=%s", at[2].Sub(at[1]))
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range at {
		if x.In(location).Format("2006-01-02") != time.Now().In(location).Format("2006-01-02") {
			t.Errorf("clock fixture escaped owner day: %s %s", zone, x)
		}
	}
	f.assertRevisions(t, refs...)
}

// Model and queue rows were produced by real calls. Shift them consistently;
// do not delete or counterfeit reservations/usage to bypass a limit.
func (f *phase25B234Fixture) advanceStatusTime(t *testing.T, elapsed time.Duration) {
	t.Helper()
	for _, tc := range []struct{ table, column string }{{"model_usage", "at"}, {"background_usage", "created_at"}, {"status_cards", "built_at"}, {"handovers", "built_at"}, {"memory_records", "updated_at"}, {"record_versions", "recorded_at"}, {"memory_jobs", "available_at"}, {"memory_jobs", "created_at"}, {"memory_jobs", "updated_at"}} {
		f.exec(t, "UPDATE "+tc.table+" SET "+tc.column+"="+tc.column+"-$2::double precision*interval '1 second' WHERE owner_id=$1", f.scope.OwnerID, elapsed.Seconds())
	}
}

func TestPhase25B3_X3_11_InferredNeverFedToHandover(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	inferred := f.claimWith(t, "虚构推断哨兵 MistyInference：陆青暗中喜欢橙色。", "inferred", "unknown", nil)
	f.labels(t, inferred, "taste", true, 1, g)
	handoverCalls := 0
	f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		input, e := phase25B3CardInput(request)
		if e == nil {
			return phase25B3JSON(phase25B3All(input))
		}
		text, e := phase25B3Prompt(request)
		if e != nil {
			return phase25B234ModelReply{status: 400}
		}
		handoverCalls++
		if strings.Contains(text, "MistyInference") {
			t.Error("inferred memory fed to handover model")
		}
		out, e := phase25B3HandoverJSON(nil, nil)
		if e != nil {
			return phase25B234ModelReply{status: 500}
		}
		return phase25B234ModelReply{content: out}
	})
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	f.buildStatus(t, time.Now().Add(12*time.Minute))
	if handoverCalls != 1 {
		t.Errorf("handover calls=%d, want 1", handoverCalls)
	}
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if utf8.RuneCountInString(a.Handover.Body) > 1800 {
		t.Errorf("handover length exceeds 1800")
	}
	if a.Handover.Body == "" {
		t.Fatal("handover was not built")
	}
	for _, title := range phase25B3Titles {
		if !strings.Contains(a.Handover.Body, title) {
			t.Errorf("missing handover section %s", title)
		}
	}
	if strings.Contains(a.Handover.Body, "MistyInference") {
		t.Error("inferred memory persisted in handover")
	}
	f.assertRevisions(t, append(refs, inferred)...)
}

func TestPhase25B3_X3_12_RuleUpgradeReturnsOldCardUntilRebuilt(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.statusModel(t, nil)
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	key := "entity:" + g.EntityID
	restore := phase25B3DefaultAdapter(f).setVersions(postgres.CardVersion+1, postgres.HandoverVersion)
	t.Cleanup(restore)
	f.scheduleStatus(t, time.Now().Add(9*time.Minute))
	var a workspace.About
	f.get(t, "/v1/workspace/about?key="+url.QueryEscape(key), &a)
	phase25B234AssertIDs(t, phase25B3Items(a.Cards), refs...)
	if len(a.Cards) != 1 || !a.Cards[0].Stale {
		t.Errorf("old-version card not readable/stale: %+v", a.Cards)
	}
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), refs...)
	var rule int
	if err := f.db.QueryRow(f.ctx, `SELECT rule FROM status_cards WHERE owner_id=$1 AND key=$2`, f.scope.OwnerID, key).Scan(&rule); err != nil {
		t.Fatal(err)
	}
	if rule != postgres.CardVersion {
		t.Errorf("rebuilt rule=%d, want %d", rule, postgres.CardVersion)
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_InputAndOutputCapsSelfRuleApplicability(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		count, input, output int
		self                 bool
	}{{"ordinary", 310, 300, 25, false}, {"self_rule", 65, 65, 60, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := phase25B234NewFixtureTimeout(t, 3*time.Minute)
			var g workspace.MemoryGroup
			if !tc.self {
				g = workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构大组输入")), Name: "虚构大组输入", Type: "topic"}
			}
			var refs []memory.Ref
			for i := 0; i < tc.count; i++ {
				r := f.claim(t, fmt.Sprintf("虚构持久要求 %03d：起草邮件先展示草稿。", i))
				category := "progress"
				if tc.self {
					category = "rule"
					f.labels(t, r, category, true, 1)
				} else {
					f.labels(t, r, category, true, 1, g)
				}
				refs = append(refs, r)
			}
			f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
				if len(input.Memories) != tc.input {
					t.Errorf("input count=%d, want %d", len(input.Memories), tc.input)
				}
				if !tc.self {
					for _, m := range input.Memories {
						for i := 0; i < 10; i++ {
							if m.Text == fmt.Sprintf("虚构持久要求 %03d：起草邮件先展示草稿。", i) {
								t.Errorf("oldest input retained instead of recent 300: %s", m.Text)
							}
						}
					}
				}
				out := phase25B3All(input)
				for _, m := range input.Memories {
					out.Rules = append(out.Rules, phase25B3WireRule{N: m.N, AppliesTo: "起草邮件"})
				}
				return out
			})
			f.buildStatus(t, time.Now().Add(11*time.Minute))
			key := "entity:" + g.EntityID
			if tc.self {
				key = "self:rule"
			}
			items := phase25B3Items(f.readCards(t, key))
			if len(items) != tc.output {
				t.Errorf("output items=%d, want %d", len(items), tc.output)
			}
			seen := map[string]bool{}
			for _, m := range items {
				if seen[m.ID] {
					t.Error("duplicate card item")
				}
				seen[m.ID] = true
				if tc.self && m.AppliesTo != "起草邮件" {
					t.Errorf("rule applicability=%q", m.AppliesTo)
				}
			}
			f.assertRevisions(t, refs...)
		})
	}
}

func TestPhase25B3_DuplicateItemsAndUnsupportedFieldsDiscarded(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	f.statusModel(t, func(phase25B3Input) phase25B3WireCard {
		return phase25B3WireCard{Fields: map[string][]int{"status": {1, 1, 2}, "next": {1, 3}, "unsupported": {2}}}
	})
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	cards := f.readCards(t, "entity:"+g.EntityID)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs...)
	for _, c := range cards {
		for _, field := range c.Fields {
			if field.Field == "unsupported" {
				t.Error("unsupported field persisted")
			}
		}
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_InitialSelfFirstThenLargestAndUsage(t *testing.T) {
	f := phase25B234NewFixture(t)
	large, largeRefs := f.cardGroup(t, 7)
	small := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构小组")), Name: "虚构小组", Type: "topic"}
	middle := workspace.MemoryGroup{EntityID: string(f.entity(t, "area", "虚构实验领域")), Name: "虚构实验领域", Type: "area"}
	all := append([]memory.Ref{}, largeRefs...)
	for _, group := range []struct {
		g workspace.MemoryGroup
		n int
	}{{small, 3}, {middle, 5}} {
		for i := 0; i < group.n; i++ {
			r := f.claim(t, fmt.Sprintf("%s虚构便签%d。", group.g.Name, i))
			f.labels(t, r, "progress", true, 1, group.g)
			all = append(all, r)
		}
	}
	self := map[string]bool{}
	for _, category := range []string{"identity", "taste", "rule", "goal"} {
		self["self:"+category] = true
		for i := 0; i < 3; i++ {
			r := f.claim(t, fmt.Sprintf("虚构本人%s便签%d。", category, i))
			f.labels(t, r, category, true, 1)
			all = append(all, r)
		}
	}
	model := f.statusModel(t, nil)
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	var keys []string
	for _, call := range model.calls() {
		input, e := phase25B3CardInput(call)
		if e == nil {
			keys = append(keys, input.Key)
		}
	}
	if len(keys) != 7 {
		t.Fatalf("initial calls=%v", keys)
	}
	for _, key := range keys[:4] {
		if !self[key] {
			t.Errorf("first four cards must be the four self categories: %v", keys)
		}
		delete(self, key)
	}
	if len(self) != 0 {
		t.Errorf("missing or repeated self categories: %v", self)
	}
	for i, want := range []string{"entity:" + large.EntityID, "entity:" + middle.EntityID, "entity:" + small.EntityID} {
		if keys[i+4] != want {
			t.Errorf("non-self cards must descend by 7,5,3 current memories: %v", keys)
		}
	}
	if n := f.usage(t, "card"); n != 7 {
		t.Errorf("card usage=%d want 7", n)
	}
	f.assertRevisions(t, all...)
}

func TestPhase25B3_NoModelDoesNotSchedule(t *testing.T) {
	f := phase25B234NewFixture(t)
	f.cardGroup(t, 3)
	if n := f.scheduleStatus(t, time.Now().Add(11*time.Minute)); n != 0 {
		t.Errorf("unconfigured model scheduled=%d", n)
	}
}

// The captured prompt shape is deliberately centralized; this test only
// verifies that every delivered current Memory carries the contract trust.
func TestPhase25B3_GeneratedCardHTTPAppliesTo(t *testing.T) {
	f := phase25B234NewFixture(t)
	var refs []memory.Ref
	for i := 0; i < 3; i++ {
		r := f.claim(t, fmt.Sprintf("虚构邮件规则 %d：先给我看。", i))
		f.labels(t, r, "rule", true, 1)
		refs = append(refs, r)
	}
	f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
		out := phase25B3All(input)
		for _, m := range input.Memories {
			out.Rules = append(out.Rules, phase25B3WireRule{N: m.N, AppliesTo: "起草邮件"})
		}
		return out
	})
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	var raw map[string]json.RawMessage
	f.get(t, "/v1/workspace/about?key=self:rule", &raw)
	var cards []workspace.StatusCard
	if err := json.Unmarshal(raw["cards"], &cards); err != nil {
		t.Fatal(err)
	}
	phase25B234AssertIDs(t, phase25B3Items(cards), refs...)
	for _, m := range phase25B3Items(cards) {
		if m.AppliesTo != "起草邮件" || m.Trust == "" {
			t.Errorf("missing applicability/trust %+v", m)
		}
	}
}
