package extraction

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// sourceStore is an in-memory SourceUnit.
type sourceStore struct {
	automatic  int
	captured   bool
	existing   bool
	imported   bool
	undone     bool
	wake       bool
	created    []workspace.Item
	candidates []workspace.Candidate
	overflow   map[string]int
	remembered []string
	signals    int
	merged     bool
	completed  bool
}

func newSourceStore() *sourceStore { return &sourceStore{overflow: map[string]int{}, wake: true} }

func (s *sourceStore) Captured(context.Context, memory.Ref, string, string, string) (bool, error) {
	return s.captured, nil
}
func (s *sourceStore) AutomaticTasks(context.Context, memory.Ref, string) (int, error) {
	return s.automatic, nil
}
func (s *sourceStore) Overflow(_ context.Context, reason string, count int) error {
	s.overflow[reason] += count
	return nil
}
func (s *sourceStore) Create(_ context.Context, item workspace.Item, _, _ string) error {
	s.created = append(s.created, item)
	return nil
}
func (s *sourceStore) SaveCandidate(_ context.Context, c workspace.Candidate) error {
	s.candidates = append(s.candidates, c)
	return nil
}
func (s *sourceStore) Current(context.Context) (bool, error)   { return true, nil }
func (s *sourceStore) Acknowledge(context.Context) error       { return nil }
func (s *sourceStore) WakeIdeas(context.Context) (bool, error) { return s.wake, nil }
func (s *sourceStore) Imported(context.Context) (bool, error)  { return s.imported, nil }
func (s *sourceStore) DeskTurn(context.Context, string) (bool, bool, error) {
	return s.undone, false, nil
}
func (s *sourceStore) CandidateExists(context.Context, string, string) (bool, error) {
	return s.existing, nil
}
func (s *sourceStore) Remember(_ context.Context, d Decision, _ memory.SourceResult, _ string, _ bool) (memory.ID, error) {
	s.remembered = append(s.remembered, d.Item.Text)
	return "m", nil
}
func (s *sourceStore) ApplySignals(_ context.Context, _ memory.Source, signals []Signal) error {
	s.signals++
	return nil
}
func (s *sourceStore) MergeCapture(context.Context) error      { s.merged = true; return nil }
func (s *sourceStore) DiscardPaidResult(context.Context) error { return nil }
func (s *sourceStore) Complete(context.Context) error          { s.completed = true; return nil }

var plain = Item{Kind: "task", Text: "交房租", Quote: "交房租", Confidence: 0.99, Explicit: true, Acquisition: "direct"}

func ownerSource() memory.SourceResult {
	source := memory.SourceResult{Context: &memory.SourceContext{Role: "user", Conversation: "c"}}
	source.Source.Ref = memory.Ref{ID: "s", Version: 1, Kind: memory.SourceKind}
	source.Source.Text = "记得周五交房租，我下周搬家"
	return source
}

func TestAPlainTaskIsMadeAndAnUncertainOneStaysACandidate(t *testing.T) {
	s := newSourceStore()
	now := time.Now()
	if err := CaptureItem(context.Background(), s, ownerSource(), plain, false, "", now); err != nil {
		t.Fatal(err)
	}
	if len(s.created) != 1 || s.candidates[0].State != "accepted" || s.candidates[0].ResolvedInto != s.created[0].ID {
		t.Fatalf("%+v", s.candidates)
	}
	unsure := plain
	unsure.Confidence = 0.9
	if err := CaptureItem(context.Background(), s, ownerSource(), unsure, true, "", now); err != nil {
		t.Fatal(err)
	}
	if c := s.candidates[1]; c.State != "pending" || strings.Join(c.Reasons, ",") != "historical,uncertain" || len(s.created) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestTasksBeyondTheLimitOrTooLongStayCountedCandidates(t *testing.T) {
	s := newSourceStore()
	s.automatic = 20
	if err := CaptureItem(context.Background(), s, ownerSource(), plain, false, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	long := plain
	long.Kind, long.Text = "idea", strings.Repeat("长", 700)
	if err := CaptureItem(context.Background(), s, ownerSource(), long, false, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(s.created) != 0 || s.overflow["automatic_task_limit"] != 1 || s.overflow["automatic_title_bytes"] != 100 {
		t.Fatalf("%v %d", s.overflow, len(s.created))
	}
	s = newSourceStore()
	s.captured = true
	if err := CaptureItem(context.Background(), s, ownerSource(), plain, false, "", time.Now()); err != nil || len(s.candidates) != 0 {
		t.Fatal("a source gave the same candidate twice")
	}
}

func TestAnAnswerRemembersCapturesAndClosesTheQuickNote(t *testing.T) {
	s := newSourceStore()
	memoryItem := Item{Kind: "memory", Nature: "plan", Text: "下周搬家", Subject: "我", Quote: "我下周搬家", Confidence: 0.95, Explicit: true, Acquisition: "direct"}
	result := Result{Source: ownerSource(), Output: Output{Items: []Item{memoryItem, plain, {Kind: "memory", Nature: "fact", Text: "没有出处", Quote: "不在原文"}}}, Location: time.UTC}
	if err := ApplySource(context.Background(), s, memory.Ref{ID: "s", Version: 1}, result, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.remembered, ",") != "下周搬家" || len(s.created) != 1 || !s.merged || !s.completed || s.signals != 1 {
		t.Fatalf("%+v", s)
	}
	if c := s.candidates[0]; c.State != "accepted" || c.ResolvedInto != "m" {
		t.Fatalf("%+v", s.candidates)
	}
}

func TestAnAnswerThatTakesNothingKeepsTheQuickNoteAndWakesNoIdeaWhenOff(t *testing.T) {
	s := newSourceStore()
	s.wake = false
	result := Result{Source: ownerSource(), Output: Output{Items: []Item{{Kind: "memory", Nature: "fact", Text: "没有出处", Quote: "不在原文"}}}, Location: time.UTC}
	if err := ApplySource(context.Background(), s, memory.Ref{ID: "s", Version: 1}, result, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.merged || s.signals != 0 || !s.completed {
		t.Fatalf("%+v", s)
	}
}
