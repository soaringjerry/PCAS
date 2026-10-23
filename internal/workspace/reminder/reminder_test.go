package reminder

import (
	"context"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// store is an in-memory Unit and SignalUnit.
type store struct {
	settings workspace.Settings
	items    []workspace.Item
	reviews  []time.Time
	notices  map[string]bool
	written  int
	saved    []workspace.Item
}

func newStore(items ...workspace.Item) *store {
	return &store{settings: workspace.Settings{Timezone: "Asia/Shanghai", DailyReviewAt: "09:00", FollowUps: true, WakeIdeas: true}, items: items, notices: map[string]bool{}}
}

func (s *store) Settings(context.Context) (workspace.Settings, error) { return s.settings, nil }
func (s *store) OpenItems(context.Context) ([]workspace.Item, error)  { return s.items, nil }
func (s *store) PendingCandidates(context.Context) (int, error)       { return 2, nil }
func (s *store) AddReview(_ context.Context, due time.Time, _ int) error {
	s.reviews = append(s.reviews, due)
	return nil
}
func (s *store) Noticed(_ context.Context, item, trigger string) (bool, error) {
	return s.notices[item+"/"+trigger], nil
}
func (s *store) AddNotice(_ context.Context, item, trigger string, _ time.Time, _ string) error {
	s.notices[item+"/"+trigger] = true
	s.written++
	return nil
}
func (s *store) SaveItem(_ context.Context, item workspace.Item) error {
	s.saved = append(s.saved, item)
	return nil
}
func (s *store) Item(_ context.Context, id string) (workspace.Item, error) {
	for _, item := range s.items {
		if item.ID == id {
			return item, nil
		}
	}
	return workspace.Item{}, memory.ErrNotFound
}
func (s *store) AddSignalNotice(_ context.Context, item, condition string, _ time.Time, _ string, _ memory.Source) (bool, error) {
	key := item + "/" + condition
	added := !s.notices[key]
	s.notices[key] = true
	return added, nil
}

// 02:00 UTC is 10:00 in Shanghai, after the review time.
var now = time.Date(2026, 3, 2, 2, 0, 0, 0, time.UTC)

func trigger(id, at, guard string) workspace.Trigger {
	return workspace.Trigger{ID: id, Active: true, NextAt: at, Guard: guard}
}

func TestTheReviewAndATaskReminderComeAtTheirTime(t *testing.T) {
	task := workspace.Item{ID: "t", Kind: "task", Status: "todo", Triggers: []workspace.Trigger{
		trigger("due", "2026-03-02T01:00:00Z", ""), trigger("later", "2026-03-02T05:00:00Z", ""), trigger("guarded", "2026-03-01T00:00:00Z", "waiting"),
	}}
	s := newStore(task)
	if err := Check(context.Background(), s, now); err != nil {
		t.Fatal(err)
	}
	if len(s.reviews) != 1 || !s.notices["t/due"] || s.notices["t/later"] || s.notices["t/guarded"] {
		t.Fatalf("%v %v", s.reviews, s.notices)
	}
	s = newStore(task)
	s.settings.FollowUps = false
	if err := Check(context.Background(), s, now.Add(-2*time.Hour)); err != nil || len(s.reviews)+len(s.notices) != 0 {
		t.Fatal("a reminder came before its review time or with follow-ups off", s.reviews, s.notices)
	}
}

func TestTheStartReminderComesOnceWhileTheTaskWaits(t *testing.T) {
	task := workspace.Item{ID: "t", Kind: "task", Status: "todo", RemindersOn: true, Triggers: []workspace.Trigger{trigger(effort.StartTrigger, "2026-03-01T00:00:00Z", "")}}
	s := newStore(task)
	if err := Check(context.Background(), s, now); err != nil || !s.notices["t/"+effort.StartTrigger] {
		t.Fatal(err, s.notices)
	}
	s.items[0].Triggers[0].NextAt = "2026-03-01T12:00:00Z"
	s.written = 0
	if err := Check(context.Background(), s, now); err != nil || s.written != 0 {
		t.Fatal("the start reminder came twice", s.notices)
	}
	doing := task
	doing.Status = "doing"
	s = newStore(doing)
	if err := Check(context.Background(), s, now); err != nil || len(s.notices) != 0 {
		t.Fatal("a task being done got its start reminder", s.notices)
	}
}

func TestAShelvedIdeaWakesByItsTimeOrByAFoundCondition(t *testing.T) {
	idea := workspace.Item{ID: "i", Kind: "idea", Status: "shelved", RemindersOn: true, Conditions: []workspace.Condition{{ID: "c", Kind: "time", DueAt: "2026-03-01T00:00:00Z", Description: "三月"}}}
	settings := newStore().settings
	woken := idea
	if !Wake(&woken, settings, now) || woken.Status != "awakened" || woken.Wake.ConditionID != "c" || woken.Evolution[0].Summary != "唤醒：三月" {
		t.Fatalf("%+v", woken)
	}
	event := idea
	event.Conditions = []workspace.Condition{{ID: "e", Kind: "event", Met: true}}
	if Wake(&event, settings, now) {
		t.Fatal("an event condition without a source woke the idea")
	}
	settings.WakeIdeas = false
	if Wake(&idea, settings, now) {
		t.Fatal("an idea woke with waking off")
	}
}

func TestASignWakesAnIdeaOnlyWithItsQuotationAndOnce(t *testing.T) {
	idea := workspace.Item{ID: "11111111-1111-4111-8111-111111111111", Kind: "idea", Status: "shelved", RemindersOn: true, Conditions: []workspace.Condition{{ID: "c", Kind: "event"}}}
	s := newStore(idea)
	source := memory.Source{Text: "听说下个月有展览", Title: "聊天"}
	signals := []extraction.Signal{
		{IdeaID: idea.ID, ConditionID: "c", Quote: "不在原文", Explanation: "展览", Confidence: 0.99},
		{IdeaID: idea.ID, ConditionID: "c", Quote: "有展览", Explanation: "展览", Confidence: 0.9},
		{IdeaID: idea.ID, ConditionID: "c", Quote: "有展览", Explanation: "展览", Confidence: 0.99},
		{IdeaID: idea.ID, ConditionID: "c", Quote: "有展览", Explanation: "展览", Confidence: 0.99},
	}
	if err := ApplySignals(context.Background(), s, source, signals, now); err != nil {
		t.Fatal(err)
	}
	if len(s.saved) != 1 || s.saved[0].Status != "awakened" || s.saved[0].Conditions[0].MetBy.Excerpt != "有展览" || s.saved[0].Conditions[0].Met {
		t.Fatalf("%+v", s.saved)
	}
	if err := ApplySignals(context.Background(), s, source, make([]extraction.Signal, 31), now); err != memory.ErrInvalid {
		t.Fatal(err)
	}
}

func TestAnUndoneTaskDoesNotRemindAgainOfAnOldTime(t *testing.T) {
	done := workspace.Item{Kind: "task", Status: "done"}
	restored := workspace.Item{Kind: "task", Status: "todo", Triggers: []workspace.Trigger{trigger("old", "2026-03-01T00:00:00Z", ""), trigger("recent", now.Add(-30*time.Minute).Format(time.RFC3339), "")}}
	if got := Suppressed(done, restored, now); len(got) != 1 || got[0].ID != "old" {
		t.Fatalf("%+v", got)
	}
}
