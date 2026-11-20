package deputy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// requestStore is an in-memory RequestUnit.
type requestStore struct {
	items     map[string]workspace.Item
	agent     workspace.Agent
	docs      map[string]workspace.Doc
	history   []Turn
	recall    memory.RecallResult
	tier      string
	ready     bool
	available bool
	cost      float64
	spent     float64
	settings  workspace.Settings
	overflow  map[string]int
	inserted  *workspace.Run
	status    string
	target    int
	since     time.Time
}

func newRequestStore() *requestStore {
	return &requestStore{
		items:     map[string]workspace.Item{"11111111-1111-4111-8111-111111111111": {ID: "11111111-1111-4111-8111-111111111111", Kind: "task", Title: "写周报"}},
		agent:     workspace.Agent{ID: "a", Enabled: true, Channel: "model"},
		docs:      map[string]workspace.Doc{},
		available: true, cost: 0.1,
		settings: workspace.Settings{Timezone: "Asia/Shanghai", DailyBudget: 1},
		overflow: map[string]int{},
	}
}

func (s *requestStore) Item(_ context.Context, id string) (workspace.Item, error) {
	item, ok := s.items[id]
	if !ok {
		return item, memory.ErrNotFound
	}
	return item, nil
}
func (s *requestStore) Agent(context.Context, string) (workspace.Agent, error) { return s.agent, nil }
func (s *requestStore) Sanitize(_ context.Context, _ string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	return item, nil, nil
}
func (s *requestStore) History(context.Context, workspace.Command, workspace.Item) ([]Turn, error) {
	return s.history, nil
}
func (s *requestStore) Memories(context.Context, string) ([]workspace.Memory, error) { return nil, nil }
func (s *requestStore) Smoke(context.Context) string                                 { return "" }
func (s *requestStore) Documents(context.Context, workspace.Item, string) ([]workspace.Doc, []memory.Ref, error) {
	return nil, nil, nil
}
func (s *requestStore) Document(_ context.Context, id string) (workspace.Doc, error) {
	doc, ok := s.docs[id]
	if !ok {
		return doc, memory.ErrNotFound
	}
	return doc, nil
}
func (s *requestStore) DocumentVersion(_ context.Context, id string, version int) (workspace.DocumentVersion, error) {
	return workspace.DocumentVersion{Title: "旧稿", Body: "旧的正文"}, nil
}
func (s *requestStore) Settings(context.Context) (workspace.Settings, error) { return s.settings, nil }
func (s *requestStore) StartUse(context.Context) (contextbuilder.UseContext, error) {
	return contextbuilder.UseContext{Ready: s.ready}, nil
}
func (s *requestStore) Recall(context.Context, string, memory.RecallRequest, memory.Budget, []string) (memory.RecallResult, error) {
	return s.recall, nil
}
func (s *requestStore) Tier(context.Context) string { return s.tier }
func (s *requestStore) FinishUse(context.Context, workspace.Agent, string, string, []workspace.Memory, *contextbuilder.UseContext) error {
	return nil
}
func (s *requestStore) Sources(_ context.Context, _, _ string, excerpts []memory.RecallExcerpt, _ []string, _, _ int) ([]memory.RecallExcerpt, error) {
	return nil, nil
}
func (s *requestStore) Evidence(context.Context, string, string, []contextbuilder.Claim, bool) ([]contextbuilder.EvidenceGroup, []string, error) {
	return nil, nil, nil
}
func (s *requestStore) Overflow(_ context.Context, reason string, count int) error {
	if count > 0 {
		s.overflow[reason] += count
	}
	return nil
}
func (s *requestStore) Available(string) bool           { return s.available }
func (s *requestStore) Estimate(string, string) float64 { return s.cost }
func (s *requestStore) SpentSince(_ context.Context, start time.Time) (float64, error) {
	s.since = start
	return s.spent, nil
}
func (s *requestStore) Insert(_ context.Context, run workspace.Run, status string, target int) error {
	s.inserted, s.status, s.target = &run, status, target
	return nil
}

const requestItem = "11111111-1111-4111-8111-111111111111"

func runRequest(s *requestStore, c workspace.Command) error {
	if c.Type == "" {
		c.Type, c.ThingID, c.AgentID = "requestRun", requestItem, "a"
	}
	now := time.Date(2026, 3, 1, 18, 0, 0, 0, time.UTC)
	return Request(context.Background(), s, c, nil, now, now)
}

func TestARunRequestIsRefusedWithoutWritingARun(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*requestStore, *workspace.Command)
		want   error
	}{
		"unknown kind":           {func(_ *requestStore, c *workspace.Command) { c.Kind = "chat" }, memory.ErrInvalid},
		"disabled agent":         {func(s *requestStore, _ *workspace.Command) { s.agent.Enabled = false }, memory.ErrForbidden},
		"too many turns":         {func(_ *requestStore, c *workspace.Command) { c.DeskTurnIDs = make([]string, HistoryTurns+1) }, memory.ErrInvalid},
		"model unavailable":      {func(s *requestStore, _ *workspace.Command) { s.available = false }, memory.ErrUnavailable},
		"over the daily budget":  {func(s *requestStore, _ *workspace.Command) { s.spent = 0.95 }, workspace.ErrBudget},
		"revision without base":  {func(_ *requestStore, c *workspace.Command) { c.Kind = "revise" }, memory.ErrInvalid},
		"revision of other item": {func(s *requestStore, c *workspace.Command) { reviseOtherItem(s, c) }, memory.ErrInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			s := newRequestStore()
			c := workspace.Command{Type: "requestRun", ThingID: requestItem, AgentID: "a", Kind: "draft", Prompt: "写初稿"}
			tc.change(s, &c)
			if err := runRequest(s, c); !errors.Is(err, tc.want) || s.inserted != nil {
				t.Fatalf("err %v, inserted %v", err, s.inserted != nil)
			}
		})
	}
}

func reviseOtherItem(s *requestStore, c *workspace.Command) {
	c.Kind, c.DocumentID, c.BaseVersion = "revise", "22222222-2222-4222-8222-222222222222", 1
	s.docs[c.DocumentID] = workspace.Doc{ID: c.DocumentID, ThingID: "33333333-3333-4333-8333-333333333333", Version: 2}
}

func TestARunReservesItsCostOnTheOwnersDay(t *testing.T) {
	s := newRequestStore()
	if err := runRequest(s, workspace.Command{Kind: "draft", Prompt: "写初稿"}); err != nil {
		t.Fatal(err)
	}
	if s.status != "queued" || s.inserted.Status != "running" || s.inserted.Cost != 0.1 || s.inserted.MemoryTier != "medium" {
		t.Fatalf("%s %+v", s.status, *s.inserted)
	}
	// 18:00 UTC is 02:00 the next day in Shanghai: the day starts at its midnight.
	if want := time.Date(2026, 3, 2, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)); !s.since.Equal(want) {
		t.Fatalf("budget day starts %v", s.since)
	}
}

func TestAManualRunWaitsAndReservesNothing(t *testing.T) {
	s := newRequestStore()
	s.agent.Channel, s.available, s.spent = "manual", false, 5
	if err := runRequest(s, workspace.Command{Kind: "draft", Prompt: "写初稿"}); err != nil {
		t.Fatal(err)
	}
	if s.status != "waiting" || s.inserted.Status != "waiting" || s.inserted.Cost != 0 {
		t.Fatalf("%s %+v", s.status, *s.inserted)
	}
}

func TestARevisionWritesItsBaseAndKeepsItsTarget(t *testing.T) {
	s := newRequestStore()
	doc := "22222222-2222-4222-8222-222222222222"
	s.docs[doc] = workspace.Doc{ID: doc, ThingID: requestItem, Version: 3}
	if err := runRequest(s, workspace.Command{Kind: "revise", Prompt: "改短", DocumentID: doc, BaseVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if s.target != 3 || !strings.Contains(s.inserted.Brief, "旧的正文") {
		t.Fatalf("target %d brief %q", s.target, s.inserted.Brief)
	}
}

func TestTheBriefEndsWithTheRequestAndCountsWhatDidNotFit(t *testing.T) {
	s := newRequestStore()
	s.recall = memory.RecallResult{TimeRelaxed: true, Coverage: memory.Coverage{Omitted: 2}}
	s.history = []Turn{{Question: strings.Repeat("旧", 2000), Answer: "好"}, {Question: "最近", Answer: "已记下", Refs: []memory.Ref{{ID: "m", Version: 1}}}}
	if err := runRequest(s, workspace.Command{Kind: "draft", Prompt: "写初稿"}); err != nil {
		t.Fatal(err)
	}
	brief := s.inserted.Brief
	if !strings.HasSuffix(brief, "\n本次请求：写初稿\n"+prompts.Must("recall-date").Text()+"\n"+OutputFormat("draft")) {
		t.Fatalf("%q", brief[max(0, len(brief)-200):])
	}
	if s.overflow["recall_budget"] != 2 || s.overflow["delegate_history_bytes"] == 0 || s.overflow["retrieval_query_bytes"] == 0 {
		t.Fatalf("%v", s.overflow)
	}
	if !strings.Contains(brief, "答：已记下") || len(s.inserted.ContextVersions) != 1 {
		t.Fatalf("the recent turn and its memory are missing: %v", s.inserted.ContextVersions)
	}
}
