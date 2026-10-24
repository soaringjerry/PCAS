package erasure

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// store is an in-memory Unit: records with versions, links from a record to
// what was derived from it, and episodes with their members.
type store struct {
	versions map[string]int
	sources  map[string][]string
	derived  map[string][]string
	answers  map[string][]string
	episodes map[string][]string
	subjects map[string]string
	items    []workspace.Item
	log      []string
	saved    []workspace.Item
	removed  []string
	receipt  string
}

func (s *store) Lock(_ context.Context, id string) (int, error) {
	v, ok := s.versions[id]
	if !ok {
		return 0, memory.ErrNotFound
	}
	return v, nil
}

func (s *store) Sources(_ context.Context, ids []string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		out = append(out, s.sources[id]...)
	}
	return out, nil
}

func (s *store) Closure(_ context.Context, ids []string, answers bool) ([]string, error) {
	out := slices.Clone(ids)
	for i := 0; i < len(out); i++ {
		for _, child := range s.derived[out[i]] {
			if !slices.Contains(out, child) {
				out = append(out, child)
			}
		}
		if answers {
			for _, child := range s.answers[out[i]] {
				if !slices.Contains(out, child) {
					out = append(out, child)
				}
			}
		}
	}
	return out, nil
}

func (s *store) WholeEpisodes(_ context.Context, ids []string) ([]string, error) {
	out := []string{}
	for episode, members := range s.episodes {
		whole := true
		for _, m := range members {
			whole = whole && slices.Contains(ids, m)
		}
		if whole && !slices.Contains(ids, episode) {
			out = append(out, episode)
		}
	}
	return out, nil
}

func (s *store) RedactArchives(context.Context, []string) error {
	s.log = append(s.log, "archives")
	return nil
}

func (s *store) LoneSubjects(_ context.Context, ids []string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		if subject, ok := s.subjects[id]; ok {
			out = append(out, subject)
		}
	}
	return out, nil
}

func (s *store) KeepAnswers(context.Context, []string) error {
	s.log = append(s.log, "keep")
	return nil
}

func (s *store) PurgeArtifacts(_ context.Context, ids []string) ([]string, error) {
	s.log = append(s.log, "purge")
	return ids, nil
}

func (s *store) Block(context.Context, []string) error {
	s.log = append(s.log, "block")
	return nil
}

func (s *store) ScrubCalls(context.Context, []string) error {
	s.log = append(s.log, "calls")
	return nil
}

func (s *store) DropAnswers(_ context.Context, _ []string, receipt string) error {
	s.log = append(s.log, "drop")
	s.receipt = receipt
	return nil
}

func (s *store) Detach(context.Context, []string) error {
	s.log = append(s.log, "detach")
	return nil
}

func (s *store) Items(context.Context) ([]workspace.Item, error) { return s.items, nil }

func (s *store) SaveItem(_ context.Context, item workspace.Item) error {
	s.saved = append(s.saved, item)
	return nil
}

func (s *store) Remove(_ context.Context, ids []string) error {
	s.removed = ids
	return nil
}

var (
	claim   = memory.NewID()
	source  = memory.NewID()
	other   = memory.NewID()
	chunk   = "chunk"
	episode = "episode"
	entity  = "entity"
	action  = "action"
)

func newStore() *store {
	return &store{
		versions: map[string]int{string(claim): 2, string(other): 1, string(source): 1},
		sources:  map[string][]string{string(claim): {string(source)}},
		derived:  map[string][]string{string(source): {chunk}},
		answers:  map[string][]string{string(claim): {action}},
		episodes: map[string][]string{episode: {string(claim)}, "shared": {string(claim), string(other)}},
		subjects: map[string]string{string(claim): entity},
	}
}

func erase(t *testing.T, s *store, in Request) {
	t.Helper()
	if err := Erase(context.Background(), s, in, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestAnErasureTakesWhatWasDerivedAnEpisodeLeftEmptyAndALoneSubject(t *testing.T) {
	s := newStore()
	erase(t, s, Request{Targets: []memory.Ref{{ID: claim}}, IncludeSources: true})
	for _, id := range []string{string(claim), string(source), chunk, episode, entity, action} {
		if !slices.Contains(s.removed, id) {
			t.Errorf("%s was not removed: %v", id, s.removed)
		}
	}
	if slices.Contains(s.removed, "shared") || slices.Contains(s.removed, string(other)) {
		t.Fatalf("an episode with another member or an unrelated memory was removed: %v", s.removed)
	}
}

func TestAnErasureWithoutSourcesKeepsTheSource(t *testing.T) {
	s := newStore()
	erase(t, s, Request{Targets: []memory.Ref{{ID: claim}}})
	if slices.Contains(s.removed, string(source)) || slices.Contains(s.removed, chunk) {
		t.Fatalf("the source went with its memory: %v", s.removed)
	}
}

func TestAnUndoKeepsTheAnswersThatUsedTheMemory(t *testing.T) {
	s := newStore()
	erase(t, s, Request{Targets: []memory.Ref{{ID: claim}}, KeepAnswers: true})
	if slices.Contains(s.removed, action) {
		t.Fatalf("the answer's action went with the undone memory: %v", s.removed)
	}
	if !slices.Equal(s.log, []string{"archives", "keep", "calls", "detach"}) {
		t.Fatalf("steps = %v", s.log)
	}
}

func TestAnErasureRemovesTheAnswersAndEmptiesTheirTurns(t *testing.T) {
	s := newStore()
	erase(t, s, Request{Targets: []memory.Ref{{ID: claim}}, BlockReimport: true})
	if !slices.Equal(s.log, []string{"archives", "purge", "block", "calls", "drop", "detach"}) {
		t.Fatalf("steps = %v", s.log)
	}
	if s.receipt == "" {
		t.Fatal("an emptied turn's receipts say nothing")
	}
}

func TestAnErasureOfAnOldVersionIsAConflict(t *testing.T) {
	s := newStore()
	err := Erase(context.Background(), s, Request{Targets: []memory.Ref{{ID: claim, Version: 1}}}, time.Now())
	if !errors.Is(err, memory.ErrConflict) || s.removed != nil {
		t.Fatalf("err = %v, removed = %v", err, s.removed)
	}
}

func TestAnErasureNamesOneToAHundredRecordsThatExist(t *testing.T) {
	s := newStore()
	many := make([]memory.Ref, Targets+1)
	for i := range many {
		many[i] = memory.Ref{ID: memory.NewID()}
	}
	for name, c := range map[string]struct {
		targets []memory.Ref
		want    error
	}{
		"none":     {nil, memory.ErrInvalid},
		"too many": {many, memory.ErrInvalid},
		"bad id":   {[]memory.Ref{{ID: "x"}}, memory.ErrInvalid},
		"missing":  {[]memory.Ref{{ID: memory.NewID()}}, memory.ErrNotFound},
	} {
		if err := Erase(context.Background(), s, Request{Targets: c.targets}, time.Now()); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if s.removed != nil {
		t.Fatalf("removed = %v", s.removed)
	}
}

func TestAnItemForgetsWhatWasErased(t *testing.T) {
	gone := string(source)
	idea := workspace.Item{ID: "idea", Status: "awakened", Version: 3,
		Creation:   &workspace.ItemCreation{Source: &workspace.SourceRef{SourceID: gone}, MemoryIDs: []string{gone, "kept"}},
		Sources:    []workspace.SourceRef{{SourceID: gone}, {SourceID: "kept"}},
		Conditions: []workspace.Condition{{ID: "c", Met: true, MetAt: "now", MetBy: &workspace.SourceRef{SourceID: gone}}},
		Wake:       &workspace.Wake{ConditionID: "c"},
		Evolution:  []workspace.Revision{{SourceID: gone}, {Summary: "owner"}},
	}
	untouched := workspace.Item{ID: "task", Sources: []workspace.SourceRef{{SourceID: "kept"}}}
	s := newStore()
	s.items = []workspace.Item{idea, untouched}
	erase(t, s, Request{Targets: []memory.Ref{{ID: claim}}, IncludeSources: true})
	if len(s.saved) != 1 {
		t.Fatalf("saved = %v", s.saved)
	}
	got := s.saved[0]
	if got.Version != 4 || got.UpdatedAt == "" || got.Status != "shelved" || got.Wake != nil {
		t.Fatalf("idea = %+v", got)
	}
	if got.Creation.Source != nil || !slices.Equal(got.Creation.MemoryIDs, []string{"kept"}) || len(got.Sources) != 1 || len(got.Evolution) != 1 {
		t.Fatalf("idea still refers to the erased source: %+v", got)
	}
	if c := got.Conditions[0]; c.Met || c.MetBy != nil || c.MetAt != "" {
		t.Fatalf("condition = %+v", c)
	}
}
