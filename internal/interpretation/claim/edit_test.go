package claim

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// editStore is an in-memory EditUnit.
type editStore struct {
	EditUnit
	claim     memory.Claim
	corrected *memory.Claim
	samples   []workspace.Sample
	uses      []memory.UseEvent
	visible   []string
}

func (s *editStore) Active(context.Context, string) error { return nil }
func (s *editStore) AgentExists(_ context.Context, id string) (bool, error) {
	return id != "ghost", nil
}
func (s *editStore) SetVisibility(_ context.Context, _ string, agents []string) error {
	s.visible = agents
	return nil
}
func (s *editStore) Read(context.Context, string) (memory.Claim, error) { return s.claim, nil }
func (s *editStore) Correct(_ context.Context, r memory.CorrectRequest) (memory.Ref, error) {
	s.corrected = &r.Replacement
	return memory.Ref{ID: r.Target.ID, Version: r.Target.Version + 1}, nil
}
func (s *editStore) Sample(_ context.Context, v workspace.Sample) error {
	s.samples = append(s.samples, v)
	return nil
}
func (s *editStore) RecordUse(_ context.Context, e memory.UseEvent) error {
	s.uses = append(s.uses, e)
	return nil
}

const memoryID = "11111111-1111-4111-8111-111111111111"

func TestAnEditMakesTheMemoryTheOwnersAndKeepsTheCorrection(t *testing.T) {
	s := &editStore{claim: memory.Claim{Value: []byte(`"喜欢咖啡"`), Acquisition: "inferred", Confirmation: "candidate"}}
	if err := Edit(context.Background(), s, workspace.Command{Type: "editMemory", ID: memoryID, Text: "喜欢茶"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if string(s.corrected.Value) != `"喜欢茶"` || s.corrected.Confirmation != "confirmed" || s.corrected.Acquisition != "direct" {
		t.Fatalf("%+v", s.corrected)
	}
	if len(s.samples) != 1 || s.samples[0].Prompt != "喜欢咖啡" || s.samples[0].Response != "喜欢茶" || len(s.uses) != 0 {
		t.Fatalf("%+v", s.samples)
	}
	if err := Edit(context.Background(), s, workspace.Command{Type: "confirmMemory", ID: memoryID, RequestID: "r"}, time.Now()); err != nil || len(s.uses) != 1 || s.uses[0].Kind != "confirmation" {
		t.Fatal(err, s.uses)
	}
	if err := Edit(context.Background(), s, workspace.Command{Type: "editMemory", ID: memoryID, Text: " "}, time.Now()); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestAMemoryIsGivenOnlyToAgentsThatExist(t *testing.T) {
	s := &editStore{}
	if err := Edit(context.Background(), s, workspace.Command{Type: "setMemoryVisibility", ID: memoryID, AgentIDs: []string{"a", "ghost"}}, time.Now()); !errors.Is(err, memory.ErrInvalid) || s.visible != nil {
		t.Fatal(err)
	}
}

func TestACorrectionNamesKnownWordsOnly(t *testing.T) {
	valid := memory.Claim{SubjectID: memoryID, Predicate: "喜欢", Value: []byte(`"茶"`), Nature: "preference", Acquisition: "direct", Confirmation: "confirmed"}
	c, change, err := CheckCorrection(memory.CorrectRequest{Replacement: valid})
	if err != nil || change != "correction" || c.ValidTime.Precision != "unknown" || c.Scope == nil {
		t.Fatal(err, change, c)
	}
	bad := valid
	bad.Nature = "mood"
	if _, _, err := CheckCorrection(memory.CorrectRequest{Replacement: bad}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}
