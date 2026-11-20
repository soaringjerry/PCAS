package contextbuilder

import (
	"context"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestGroupsAreProposedByTheRankOfTheirRecalledMemories(t *testing.T) {
	index := []workspace.StatusCardRef{{Key: "entity:a"}, {Key: "entity:b"}, {Key: "entity:c"}, {Key: "self:rule"}}
	ranked := []workspace.Memory{
		{Groups: []workspace.MemoryGroup{{EntityID: "b"}}, Category: "rule"},
		{Groups: []workspace.MemoryGroup{{EntityID: "a"}}},
	}
	if got := strings.Join(ChooseGroups(index, ranked, 5), ","); got != "entity:b,entity:a" {
		t.Fatalf("%s", got)
	}
	if got := ChooseGroups(index, ranked, 1); len(got) != 1 || got[0] != "entity:b" {
		t.Fatalf("%v", got)
	}
}

// useStore is an in-memory UseUnit.
type useStore struct {
	requirements []workspace.AssistantRequirement
	memories     map[string]workspace.Memory
	repeated     []string
	overflow     map[string]int
}

func (s *useStore) VisibleGroups(context.Context, workspace.Agent, *string) ([]workspace.StatusCardRef, error) {
	return nil, nil
}
func (s *useStore) Studio(context.Context) (bool, error) { return false, nil }
func (s *useStore) Item(context.Context, string) (workspace.Item, error) {
	return workspace.Item{}, nil
}
func (s *useStore) ProjectHandover(context.Context, string) (workspace.ProjectHandover, error) {
	return workspace.ProjectHandover{}, nil
}
func (s *useStore) ProjectGroups(context.Context, string) ([]string, error) { return nil, nil }
func (s *useStore) Requirements(context.Context) (workspace.AssistantRequirementList, error) {
	return workspace.AssistantRequirementList{Items: s.requirements}, nil
}
func (s *useStore) Deadlines(context.Context) (workspace.DeadlineList, error) {
	return workspace.DeadlineList{}, nil
}
func (s *useStore) Memories(_ context.Context, _ workspace.Agent, _ *string, ids []string) ([]workspace.Memory, error) {
	out := []workspace.Memory{}
	for _, id := range ids {
		if m, ok := s.memories[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *useStore) Repeated(context.Context, []string) ([]string, error) { return s.repeated, nil }
func (s *useStore) Overflow(_ context.Context, reason string, count int) error {
	s.overflow[reason] += count
	return nil
}

func TestRequirementsForEveryTurnFitTheirRoomAndScopedOnesWaitForRecall(t *testing.T) {
	s := &useStore{memories: map[string]workspace.Memory{
		"new":       {ID: "new", Version: 1, Text: "回复用中文", ExpressedAt: "2026-03-02"},
		"confirmed": {ID: "confirmed", Version: 1, Text: "称呼我小林", Confirmation: "confirmed", ExpressedAt: "2026-01-01"},
		"repeated":  {ID: "repeated", Version: 1, Text: "别用表情", ExpressedAt: "2026-01-02"},
		"long":      {ID: "long", Version: 1, Text: strings.Repeat("长", globalRequirementRunes), ExpressedAt: "2026-01-03"},
		"scoped":    {ID: "scoped", Version: 1, Text: "报销单写部门"},
		"recalled":  {ID: "recalled", Version: 2, Text: "下周搬家"},
	}, repeated: []string{"repeated"}, overflow: map[string]int{}}
	for _, id := range []string{"new", "confirmed", "repeated", "long"} {
		s.requirements = append(s.requirements, workspace.AssistantRequirement{MemoryID: id, Unrestricted: true})
	}
	s.requirements = append(s.requirements, workspace.AssistantRequirement{MemoryID: "scoped"})
	use := UseContext{Installed: true}
	if err := FinishUse(context.Background(), s, workspace.Agent{}, nil, []workspace.Memory{{ID: "recalled"}, {ID: "confirmed"}}, &use); err != nil {
		t.Fatal(err)
	}
	order := []string{}
	for _, m := range use.Rules {
		order = append(order, m.ID)
	}
	if strings.Join(order, ",") != "confirmed,repeated,new" || use.RequirementScopes["new"] != unrestrictedScope {
		t.Fatalf("%v", order)
	}
	if s.overflow["scoped_requirement_left_to_recall"] != 1 || s.overflow["global_requirement_char_budget"] != 1 {
		t.Fatalf("%v", s.overflow)
	}
	if len(use.Supplemental) != 1 || use.Supplemental[0].ID != "recalled" || len(use.Dependencies) != 4 || use.Dependencies[3] != (memory.Ref{ID: "recalled", Version: 2, Kind: memory.ClaimKind}) {
		t.Fatalf("%+v %v", use.Supplemental, use.Dependencies)
	}
}
