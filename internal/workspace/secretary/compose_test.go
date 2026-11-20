package secretary

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// contextStore is an in-memory ContextUnit.
type contextStore struct {
	models    bool
	agents    map[string]workspace.Agent
	available bool
	items     map[string]workspace.Item
	tasks     []workspace.Item
	projects  []workspace.Item
	hidden    map[string]bool
	open      map[string]int
	recent    int
	turns     int
	limits    map[string]int
	overflow  map[string]int
}

func newContextStore() *contextStore {
	s := &contextStore{models: true, available: true, agents: map[string]workspace.Agent{"default": {ID: "default", Name: "秘书", Enabled: true, Channel: "model"}}, items: map[string]workspace.Item{}, hidden: map[string]bool{}, limits: map[string]int{}, overflow: map[string]int{}}
	for _, item := range []workspace.Item{{ID: "p1", Kind: "project", Title: "搬家"}, {ID: "t1", Kind: "task", Title: "订车", ProjectID: "p1"}, {ID: "t2", Kind: "task", Title: "私密", Notes: "细节"}} {
		s.items[item.ID] = item
	}
	s.projects = []workspace.Item{s.items["p1"]}
	s.tasks = []workspace.Item{s.items["t1"], s.items["t2"]}
	s.hidden["t2"] = true
	return s
}

func (s *contextStore) DefaultAgent() (string, bool) { return "default", s.models }
func (s *contextStore) Agent(_ context.Context, id string) (workspace.Agent, error) {
	agent, ok := s.agents[id]
	if !ok {
		return agent, memory.ErrNotFound
	}
	return agent, nil
}
func (s *contextStore) Available(string) bool { return s.available }
func (s *contextStore) Settings(context.Context) (workspace.Settings, error) {
	return workspace.Settings{Timezone: "UTC"}, nil
}
func (s *contextStore) OpenTasks(_ context.Context, _ string, limit int) ([]workspace.Item, []memory.Ref, error) {
	s.limits["task"] = limit
	return append([]workspace.Item{}, s.tasks...), []memory.Ref{{ID: "m", Version: 1}}, nil
}
func (s *contextStore) ActiveProjects(_ context.Context, limit int) ([]workspace.Item, error) {
	s.limits["project"] = limit
	return append([]workspace.Item{}, s.projects...), nil
}
func (s *contextStore) RecentIdeas(_ context.Context, limit int) ([]workspace.Item, error) {
	s.limits["idea"] = limit
	return nil, nil
}
func (s *contextStore) RecentItems(_ context.Context, _ string, limit int) ([]workspace.Item, error) {
	s.limits["recent"] = limit
	return nil, nil
}
func (s *contextStore) Item(_ context.Context, id string) (workspace.Item, error) {
	item, ok := s.items[id]
	if !ok {
		return item, memory.ErrNotFound
	}
	return item, nil
}
func (s *contextStore) Sanitize(_ context.Context, _ string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	if s.hidden[item.ID] {
		item.Title, item.Notes = "（不可见）", ""
	}
	return item, nil, nil
}
func (s *contextStore) OpenTasksByProject(context.Context) (map[string]int, error) {
	return map[string]int{"p1": 1}, nil
}
func (s *contextStore) Turns(_ context.Context, _ string, limit int, _ string, _ *string, _ *[]memory.Ref) ([]workspace.SecretaryTurn, error) {
	s.limits["turns"] = limit
	return []workspace.SecretaryTurn{{Text: "上一句"}}, nil
}
func (s *contextStore) Open(context.Context) (map[string]int, error) { return s.open, nil }
func (s *contextStore) RecentItemCount(context.Context, string) (int, error) {
	return s.recent, nil
}
func (s *contextStore) TurnCount(context.Context, string) (int, error) { return s.turns, nil }
func (s *contextStore) Overflow(_ context.Context, reason string, count int) error {
	if count > 0 {
		s.overflow[reason] += count
	}
	return nil
}

func TestATurnNeedsAnEnabledAgentThatReachesAModel(t *testing.T) {
	for name, change := range map[string]func(*contextStore, *workspace.DeskTurnRequest){
		"no model configured": func(s *contextStore, _ *workspace.DeskTurnRequest) { s.models = false },
		"unknown agent":       func(_ *contextStore, r *workspace.DeskTurnRequest) { r.AgentID = "gone" },
		"disabled agent": func(s *contextStore, _ *workspace.DeskTurnRequest) {
			s.agents["default"] = workspace.Agent{ID: "default"}
		},
		"manual agent": func(s *contextStore, _ *workspace.DeskTurnRequest) {
			s.agents["default"] = workspace.Agent{ID: "default", Enabled: true, Channel: "manual"}
		},
		"model unavailable": func(s *contextStore, _ *workspace.DeskTurnRequest) { s.available = false },
	} {
		t.Run(name, func(t *testing.T) {
			s, req := newContextStore(), workspace.DeskTurnRequest{Text: "你好"}
			change(s, &req)
			if _, err := Gather(context.Background(), s, req, "c"); !errors.Is(err, memory.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

func TestATurnShowsItemsByAliasAsTheAgentMaySeeThem(t *testing.T) {
	s := newContextStore()
	thing := "t1"
	c, err := Gather(context.Background(), s, workspace.DeskTurnRequest{Text: "改一下", ThingID: &thing}, "c")
	if err != nil {
		t.Fatal(err)
	}
	if c.Aliases["P1"].ID != "p1" || c.Aliases["T1"].ID != "t1" || c.Aliases["THIS"].ID != "t1" {
		t.Fatalf("%v", c.Aliases)
	}
	// The model sees the hidden task without its text; an action still finds the original.
	if c.Aliases["T2"].Title != "（不可见）" || c.TargetItems["t2"].Title != "私密" {
		t.Fatalf("%v %v", c.Aliases["T2"], c.TargetItems["t2"])
	}
	if len(c.History) != 1 || c.Counts["p1"] != 1 || len(c.Dependencies) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestWhatATurnLeavesOutIsCounted(t *testing.T) {
	s := newContextStore()
	s.open = map[string]int{"task": 45, "project": 50, "idea": 23}
	s.recent, s.turns = 21, 9
	if _, err := Gather(context.Background(), s, workspace.DeskTurnRequest{Text: "你好"}, "c"); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"task_context_limit": 5, "idea_context_limit": 3, "recent_item_context_limit": 1, "history_context_limit": 3}
	if len(s.overflow) != len(want) {
		t.Fatalf("%v", s.overflow)
	}
	for reason, n := range want {
		if s.overflow[reason] != n {
			t.Fatalf("%v", s.overflow)
		}
	}
	if s.limits["task"] != 40 || s.limits["project"] != 50 || s.limits["idea"] != 20 || s.limits["recent"] != 20 || s.limits["turns"] != 6 {
		t.Fatalf("the reads and the counts use other limits: %v", s.limits)
	}
}
