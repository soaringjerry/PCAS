package access

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// store is an in-memory Unit: one agent, the memories granted to it, the
// sources it sees, and the exclusions of the items.
type store struct {
	agent    workspace.Agent
	claims   map[string]Claim
	sources  map[string]int
	excluded map[string][]string
}

func (s *store) Agent(context.Context, string) (workspace.Agent, error) { return s.agent, nil }
func (s *store) Excluded(_ context.Context, item string) ([]string, error) {
	return s.excluded[item], nil
}
func (s *store) Claims(_ context.Context, _ string, ids []string) (map[string]Claim, error) {
	out := map[string]Claim{}
	for _, id := range ids {
		if c, ok := s.claims[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}
func (s *store) Sources(_ context.Context, _ string, _ *string, ids []string) (map[string]int, error) {
	out := map[string]int{}
	for _, id := range ids {
		if v, ok := s.sources[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

var (
	fact   = memory.NewID()
	source = memory.NewID()
)

func newStore() *store {
	return &store{
		agent:    workspace.Agent{ID: "agent", Enabled: true, MemoryKinds: []string{"fact"}},
		claims:   map[string]Claim{string(fact): {Version: 2, Nature: "fact", Acquisition: "direct"}},
		sources:  map[string]int{string(source): 1},
		excluded: map[string][]string{},
	}
}

func run() workspace.Run {
	return workspace.Run{AgentID: "agent", ContextVersions: []memory.Ref{{ID: fact, Version: 2, Kind: memory.ClaimKind}, {ID: source, Version: 1, Kind: memory.SourceKind}}}
}

func project(id string) map[string]json.RawMessage {
	return map[string]json.RawMessage{"project_id": json.RawMessage(`"` + id + `"`)}
}

func TestARunMayUseWhatItsAgentStillReads(t *testing.T) {
	if err := Verify(context.Background(), newStore(), run(), nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestARunOfADisabledAgentIsForbidden(t *testing.T) {
	s := newStore()
	s.agent.Enabled = false
	if err := Verify(context.Background(), s, run(), nil, false); !errors.Is(err, memory.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestARunMadeFromWhatChangedOrIsNoLongerReadableIsAConflict(t *testing.T) {
	item := &workspace.Item{ID: "task", Kind: "task", ProjectID: "p1"}
	for name, c := range map[string]struct {
		change func(*store)
		item   *workspace.Item
	}{
		"grant removed":  {func(s *store) { delete(s.claims, string(fact)) }, nil},
		"memory changed": {func(s *store) { s.claims[string(fact)] = Claim{Version: 3, Nature: "fact"} }, nil},
		"source changed": {func(s *store) { s.sources[string(source)] = 2 }, nil},
		"source hidden":  {func(s *store) { delete(s.sources, string(source)) }, nil},
		"kind not read":  {func(s *store) { s.agent.MemoryKinds = []string{"rule"} }, nil},
		"inferred":       {func(s *store) { s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Acquisition: "inferred"} }, nil},
		"from the AI":    {func(s *store) { s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", AI: true} }, nil},
		"superseded":     {func(s *store) { s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Retired: "superseded"} }, nil},
		"merged away":    {func(s *store) { s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Retired: "duplicate"} }, nil},
		"excluded":       {func(s *store) { s.excluded["task"] = []string{string(fact)} }, item},
		"other project":  {func(s *store) { s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Scope: project("p2")} }, item},
		"unreadable scope": {func(s *store) {
			s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Scope: map[string]json.RawMessage{"project_id": json.RawMessage(`7`)}}
		}, item},
	} {
		s := newStore()
		c.change(s)
		if err := Verify(context.Background(), s, run(), c.item, false); !errors.Is(err, memory.ErrConflict) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestARunMayUseAMemoryOfItsOwnProjectOrMergedIntoACurrentOne(t *testing.T) {
	s := newStore()
	s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Scope: project("p1"), Retired: "duplicate", KeeperCurrent: true}
	if err := Verify(context.Background(), s, run(), &workspace.Item{ID: "p1", Kind: "project"}, false); err != nil {
		t.Fatal(err)
	}
	s.agent.IncludeInferred = true
	s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", AI: true}
	if err := Verify(context.Background(), s, run(), nil, false); err != nil {
		t.Fatalf("an agent that reads inferred memory: %v", err)
	}
}

func TestARecheckedAnswerKeepsAccessToARetiredMemory(t *testing.T) {
	s := newStore()
	s.claims[string(fact)] = Claim{Version: 2, Nature: "fact", Retired: "superseded"}
	if err := Verify(context.Background(), s, run(), nil, true); err != nil {
		t.Fatal(err)
	}
}

func TestAWithheldAnswerLeavesTheItem(t *testing.T) {
	item := workspace.Item{Title: "Write", Notes: "n", Body: "b", Progress: "p", Checklist: []workspace.Check{{ID: "c1"}, {ID: "c2"}}}
	for _, kind := range []string{"notes", "body", "progress", "task"} {
		if !Withhold(&item, kind, "", false) {
			t.Errorf("%s was not used", kind)
		}
	}
	if !Withhold(&item, "check", "c1", false) {
		t.Error("the check was not used")
	}
	if item.Notes != "" || item.Body != "" || item.Progress != "" || item.Title != workspace.ReauthorizeTitle || len(item.Checklist) != 1 || item.Checklist[0].ID != "c2" {
		t.Fatalf("item = %+v", item)
	}
	kept := workspace.Item{Notes: "n"}
	if !Withhold(&kept, "notes", "", true) || kept.Notes != "n" {
		t.Fatalf("an allowed answer was withheld: %+v", kept)
	}
}
