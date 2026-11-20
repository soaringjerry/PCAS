package extraction

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// conversationStore is an in-memory ConversationUnit. log records each write.
type conversationStore struct {
	sources   []memory.SourceResult
	extracted bool
	earlier   []EarlierMemory
	blocked   map[string]bool
	log       []string
	states    [][]StateTarget
}

func (s *conversationStore) Sources(context.Context, string) ([]memory.SourceResult, error) {
	return s.sources, nil
}
func (s *conversationStore) Extracted(context.Context, string) (bool, error) { return s.extracted, nil }
func (s *conversationStore) Enqueue(_ context.Context, anchor memory.Ref, _ string, segment, next int) error {
	s.log = append(s.log, fmt.Sprintf("enqueue %s %d %d", anchor.ID, segment, next))
	return nil
}
func (s *conversationStore) ReplaceMemories(context.Context, []memory.SourceResult) error {
	s.log = append(s.log, "replace")
	return nil
}
func (s *conversationStore) Withdraw(_ context.Context, claim memory.Ref) error {
	s.log = append(s.log, "withdraw "+string(claim.ID))
	return nil
}
func (s *conversationStore) WriteStates(_ context.Context, targets []StateTarget, state string) error {
	s.states = append(s.states, targets)
	s.log = append(s.log, fmt.Sprintf("states %d %s", len(targets), state))
	return nil
}
func (s *conversationStore) RetireJobs(context.Context, string, string) error {
	s.log = append(s.log, "retire")
	return nil
}
func (s *conversationStore) Acknowledge(context.Context) error {
	s.log = append(s.log, "ack")
	return nil
}
func (s *conversationStore) DiscardPaidResult(context.Context) error {
	s.log = append(s.log, "discard")
	return nil
}
func (s *conversationStore) Earlier(context.Context, string) ([]EarlierMemory, error) {
	return s.earlier, nil
}
func (s *conversationStore) Timezone(context.Context) (string, error)           { return "UTC", nil }
func (s *conversationStore) Imported(context.Context, memory.Ref) (bool, error) { return true, nil }
func (s *conversationStore) Capture(_ context.Context, _ memory.SourceResult, item Item, _ bool) error {
	s.log = append(s.log, "capture "+item.Kind)
	return nil
}
func (s *conversationStore) Remember(_ context.Context, d Decision, _ memory.SourceResult, _ string, next int, newAtQuote bool) (bool, error) {
	if s.blocked[d.Item.Text] {
		return false, memory.ErrBlocked
	}
	s.log = append(s.log, fmt.Sprintf("remember %s %d %v", d.Item.Text, next, newAtQuote))
	return true, nil
}

func convMessage(id, role, branch, text string) memory.SourceResult {
	source := memory.SourceResult{Context: &memory.SourceContext{Role: role, Branch: branch, Conversation: "c"}}
	source.Source.Ref = memory.Ref{ID: memory.ID(id), Version: 1, Kind: memory.SourceKind}
	source.Source.Text = text
	return source
}

func conversation() []memory.SourceResult {
	return []memory.SourceResult{
		convMessage("u1", "user", "", "我下周搬家"),
		convMessage("a1", "assistant", "", "需要我帮你列清单吗"),
		convMessage("u2", "user", "", "好，顺便提醒我交房租"),
		convMessage("h1", "user", "historical", "旧分支"),
	}
}

func TestAMessageJobStartsTheRunOfItsConversationOnce(t *testing.T) {
	s := &conversationStore{sources: conversation()}
	work, err := PrepareConversation(context.Background(), s, worker.Job{Stage: Stage}, "c")
	if err != nil || work != nil || !reflect.DeepEqual(s.log, []string{"enqueue u1 0 1", "retire", "ack"}) {
		t.Fatal(err, work, s.log)
	}
	s = &conversationStore{sources: conversation(), extracted: true}
	if _, err := PrepareConversation(context.Background(), s, worker.Job{Stage: Stage}, "c"); err != nil || !reflect.DeepEqual(s.log, []string{"retire", "ack"}) {
		t.Fatal("an extracted conversation is extracted again", err, s.log)
	}
}

func TestAConversationWithoutMessagesLosesItsAutomaticMemories(t *testing.T) {
	s := &conversationStore{sources: []memory.SourceResult{convMessage("h1", "user", "historical", "旧")}}
	if _, err := PrepareConversation(context.Background(), s, worker.Job{Stage: Stage}, "c"); err != nil || !reflect.DeepEqual(s.log, []string{"replace", "states 1 ", "retire", "ack"}) {
		t.Fatal(err, s.log)
	}
	if s.states[0][0].Counted {
		t.Fatal("a historical message counts memories")
	}
}

func TestASegmentJobOfAChangedConversationStartsAgain(t *testing.T) {
	s := &conversationStore{sources: conversation()}
	stale := ConversationStage(strings.Repeat("0", 64), 0, 1)
	if work, err := PrepareConversation(context.Background(), s, worker.Job{Stage: stale}, "c"); err != nil || work != nil || !reflect.DeepEqual(s.log, []string{"enqueue u1 0 1", "ack"}) {
		t.Fatal(err, work, s.log)
	}
}

func prepared(t *testing.T, s *conversationStore, segment, next int) ConversationSegmentWork {
	t.Helper()
	work, err := PrepareConversation(context.Background(), s, worker.Job{Stage: ConversationStage(ConversationRun(s.sources), segment, next)}, "c")
	if err != nil || work == nil {
		t.Fatal(err, work)
	}
	return *work
}

func TestTheLastSegmentWritesItsItemsAndFinishesTheRun(t *testing.T) {
	s := &conversationStore{sources: conversation(), blocked: map[string]bool{"删过的": true}}
	work := prepared(t, s, 0, 3)
	if !work.HasOwnerText() || !strings.Contains(work.Prompt(), "我下周搬家") || strings.Contains(work.Prompt(), "earlier_memories") {
		t.Fatal(work.Prompt())
	}
	output := ConversationOutput{Items: []ConversationItem{
		{MessageIndex: 1, Item: Item{Kind: "memory", Text: "用户下周搬家", Nature: "plan", Subject: "我", Quote: "下周搬家", Confidence: 0.9}},
		{MessageIndex: 1, Item: Item{Kind: "memory", Text: "删过的", Nature: "plan", Subject: "我", Quote: "下周搬家", Confidence: 0.9}},
		{MessageIndex: 1, Item: Item{Kind: "memory", Text: "用户要搬家", Nature: "plan", Subject: "我", Quote: "下周搬家", Confidence: 0.9}},
		{MessageIndex: 2, Item: Item{Kind: "memory", Text: "助手说的", Nature: "fact", Subject: "我", Quote: "清单", Confidence: 0.9}},
		{MessageIndex: 3, Item: Item{Kind: "task", Text: "交房租", Quote: "交房租", Confidence: 0.9}},
	}}
	s.log = nil
	if err := ApplyConversation(context.Background(), s, work, output); err != nil {
		t.Fatal(err)
	}
	want := []string{"replace", "remember 用户下周搬家 3 false", "remember 用户要搬家 4 true", "capture task", "states 4 ", "retire", "states 4 ", "retire", "discard", "ack"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("%q", s.log)
	}
}

func TestALaterSegmentWithdrawsOnlyWhatTheOwnerLeftAlone(t *testing.T) {
	long := strings.Repeat("长", 12001)
	sources := []memory.SourceResult{convMessage("u1", "user", "", "第一段"), convMessage("u2", "user", "", long)}
	s := &conversationStore{sources: sources, earlier: []EarlierMemory{{Ref: 1, Text: "旧的", Claim: memory.Ref{ID: "m1"}}}}
	work := prepared(t, s, 1, 2)
	if !strings.Contains(work.Prompt(), "earlier_memories") {
		t.Fatal("a later segment does not read the earlier memories")
	}
	s.log = nil
	if err := ApplyConversation(context.Background(), s, work, ConversationOutput{Withdraw: []int{1, 7}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.log[:2], []string{"withdraw m1", "states 0 "}) || s.log[3] != "enqueue u2 2 2" {
		t.Fatalf("%q", s.log)
	}
}

func TestAConversationThatChangedWhileTheModelWasAskedStartsAgain(t *testing.T) {
	s := &conversationStore{sources: conversation()}
	work := prepared(t, s, 0, 1)
	s.sources = append(conversation(), convMessage("u3", "user", "", "又说了一句"))
	s.log = nil
	if err := ApplyConversation(context.Background(), s, work, ConversationOutput{}); err != nil || !reflect.DeepEqual(s.log, []string{"enqueue u1 0 1", "ack"}) {
		t.Fatal(err, s.log)
	}
}
