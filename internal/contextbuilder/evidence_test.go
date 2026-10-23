package contextbuilder

import (
	"context"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// evidenceStore is an in-memory EvidenceSource. Each claim's evidence is in
// the source named by anchors; windows holds each source's messages.
type evidenceStore struct {
	anchors map[memory.ID]EvidenceAnchor
	windows map[memory.ID][]memory.ConversationMessage
	reads   int
}

func (s *evidenceStore) Anchor(_ context.Context, ref memory.Ref) (EvidenceAnchor, bool, error) {
	a, ok := s.anchors[ref.ID]
	return a, ok, nil
}

func (s *evidenceStore) Window(_ context.Context, source memory.Ref) (memory.ConversationResult, error) {
	s.reads++
	messages, ok := s.windows[source.ID]
	if !ok {
		return memory.ConversationResult{}, memory.ErrForbidden
	}
	return memory.ConversationResult{Messages: append([]memory.ConversationMessage{}, messages...)}, nil
}

func source(id string) memory.Ref {
	return memory.Ref{ID: memory.ID(id), Version: 1, Kind: memory.SourceKind}
}

func TestTwoMemoriesOfOneConversationShareItsWindow(t *testing.T) {
	s := &evidenceStore{
		anchors: map[memory.ID]EvidenceAnchor{"m1": {Source: source("s")}, "m2": {Source: source("s")}},
		windows: map[memory.ID][]memory.ConversationMessage{"s": {{Text: "周五见", Anchor: true}}},
	}
	groups, gaps, err := Evidence(context.Background(), s, []Claim{{Label: "M1", Ref: memory.Ref{ID: "m1"}}, {Label: "M2", Ref: memory.Ref{ID: "m2"}}})
	if err != nil || len(groups) != 1 || groups[0].Label != "M1, M2" || len(gaps) != 0 || s.reads != 1 {
		t.Fatalf("%v %v %v", groups, gaps, err)
	}
}

func TestACutQuoteIsNotSharedAndSaysItIsCut(t *testing.T) {
	long := strings.Repeat("长", 2000)
	s := &evidenceStore{
		anchors: map[memory.ID]EvidenceAnchor{"m1": {Source: source("s"), Start: 1000, End: 1010}, "m2": {Source: source("s"), Start: 1500, End: 1510}},
		windows: map[memory.ID][]memory.ConversationMessage{"s": {{Text: long, Anchor: true}}},
	}
	groups, _, err := Evidence(context.Background(), s, []Claim{{Label: "M1", Ref: memory.Ref{ID: "m1"}}, {Label: "M2", Ref: memory.Ref{ID: "m2"}}})
	if err != nil || len(groups) != 2 {
		t.Fatalf("%v %v", groups, err)
	}
	if text := groups[0].Window.Messages[0].Text; len([]rune(text)) != 900+len([]rune(clippedMessage)) || !strings.HasSuffix(text, clippedMessage) {
		t.Fatalf("%q", text[len(text)-60:])
	}
}

func TestAMemoryThatNeedsItsContextGoesFirstAndAMissingOneIsSaid(t *testing.T) {
	s := &evidenceStore{
		anchors: map[memory.ID]EvidenceAnchor{"plain": {Source: source("a")}, "vague": {Source: source("b")}},
		windows: map[memory.ID][]memory.ConversationMessage{"a": {{Text: "一", Anchor: true}}, "b": {{Text: "二", Anchor: true}}},
	}
	claims := []Claim{{Label: "M1", Ref: memory.Ref{ID: "plain"}, Text: "周五交房租"}, {Label: "M2", Ref: memory.Ref{ID: "vague"}, Text: "他说那个要改"}, {Label: "M3", Ref: memory.Ref{ID: "none"}, Text: "对方不同意"}}
	groups, gaps, err := Evidence(context.Background(), s, claims)
	if err != nil || len(groups) != 2 || groups[0].Label != "M2" {
		t.Fatalf("%v %v", groups, err)
	}
	if len(gaps) != 1 || !strings.HasPrefix(gaps[0], "M3 含未明确指代") {
		t.Fatalf("%v", gaps)
	}
}

func TestEvidenceBeyondItsLimitsIsSaidNotGuessed(t *testing.T) {
	s := &evidenceStore{anchors: map[memory.ID]EvidenceAnchor{}, windows: map[memory.ID][]memory.ConversationMessage{}}
	claims := []Claim{}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		s.anchors[memory.ID(id)] = EvidenceAnchor{Source: source(id)}
		s.windows[memory.ID(id)] = []memory.ConversationMessage{{Text: "短", Anchor: true}}
		claims = append(claims, Claim{Label: strings.ToUpper(id), Ref: memory.Ref{ID: memory.ID(id)}})
	}
	s.anchors["hidden"] = EvidenceAnchor{Source: source("hidden")}
	claims = append([]Claim{{Label: "H", Ref: memory.Ref{ID: "hidden"}}}, claims...)
	groups, gaps, err := Evidence(context.Background(), s, claims)
	if err != nil || len(groups) != evidenceGroups {
		t.Fatalf("%d %v", len(groups), err)
	}
	if len(gaps) != 2 || !strings.HasPrefix(gaps[0], "H 的证据上下文不可用") || !strings.HasPrefix(gaps[1], "E 的来源上下文尚未补齐") {
		t.Fatalf("%v", gaps)
	}
}
