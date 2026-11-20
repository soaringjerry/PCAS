package organize

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// applyStore is an in-memory ApplyUnit; log records each write.
type applyStore struct {
	stale     map[memory.ID]bool
	withdrawn int
	removed   int
	log       []string
}

func (s *applyStore) Current(_ context.Context, ref memory.Ref, _ int) (bool, error) {
	return !s.stale[ref.ID], nil
}
func (s *applyStore) Groups(_ context.Context, items map[int]Item, _ []Group, current map[int]bool) (map[int][]Group, error) {
	out := map[int][]Group{}
	for n := range current {
		out[n] = items[n].Groups
	}
	return out, nil
}
func (s *applyStore) Defer(_ context.Context, id memory.ID) error {
	s.log = append(s.log, "defer "+string(id))
	return nil
}
func (s *applyStore) Count(_ context.Context, outcome, reason string, n int) error {
	s.log = append(s.log, fmt.Sprintf("%s %s %d", outcome, reason, n))
	return nil
}
func (s *applyStore) Label(_ context.Context, ref memory.Ref, category string, _ *bool) error {
	s.log = append(s.log, "label "+string(ref.ID)+" "+category)
	return nil
}
func (s *applyStore) SetGroups(context.Context, memory.Ref, []Group) error { return nil }
func (s *applyStore) Dates(context.Context, Memory, []Deadline, string) (int, error) {
	return s.withdrawn, nil
}
func (s *applyStore) Requirement(_ context.Context, ref memory.Ref, rule, _ bool, _ string) (int, error) {
	if rule {
		s.log = append(s.log, "rule "+string(ref.ID))
	}
	return s.removed, nil
}
func (s *applyStore) Organized(_ context.Context, id memory.ID, _ int) error {
	s.log = append(s.log, "organized "+string(id))
	return nil
}
func (s *applyStore) Finish(context.Context) error { s.log = append(s.log, "finish"); return nil }

func TestABatchLabelsCurrentStatementsAndDefersTheOnesWithoutAnAnswer(t *testing.T) {
	s := &applyStore{stale: map[memory.ID]bool{"c": true}, removed: 1}
	result := Result{Version: 3, Memories: []Memory{{N: 1, Ref: memory.Ref{ID: "a"}}, {N: 2, Ref: memory.Ref{ID: "b"}}, {N: 3, Ref: memory.Ref{ID: "c"}}},
		Items: map[int]Item{1: {Category: "taste"}, 3: {Category: "rule"}}}
	applied, err := Apply(context.Background(), s, result)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"label a taste", "success reorganize_requirements_withdrawn 1", "organized a", "defer b", "failure organize_invalid_output 1", "finish"}
	if !reflect.DeepEqual(s.log, want) || len(applied.Deferred) != 1 || applied.Deferred[0] != "b" {
		t.Fatalf("%q %v", s.log, applied)
	}
}
