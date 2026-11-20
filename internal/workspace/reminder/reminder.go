// Package reminder owns when the owner is reminded: the daily review, the
// reminders of a task, and the waking of a shelved idea by its time or by a
// sign in a new source. Explicit times and acknowledged conditions are
// evaluated; a condition in words stays open until there is evidence, and
// never becomes a keyword trigger or an age-based nag. Storage gives the
// reads and writes of one owner in one transaction.
package reminder

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// A sign of a model wakes an idea only with this confidence and a quotation
// from the source. One source gives at most signalLimit signs.
const (
	signalConfidence  = 0.98
	signalLimit       = 30
	explanationLength = 2000
)

// The extraction of a source looks for the event conditions of the latest
// shelved ideas, at most these many ideas and conditions.
const (
	PendingIdeas      = 20
	pendingConditions = 30
)

// restoredGrace is how long a reminder may have passed before an undo
// restores its task. An older one is not sent again.
const restoredGrace = time.Hour

// Unit is the storage of the reminders of one owner, in one transaction.
// Each method reads or writes; none decides.
type Unit interface {
	Settings(ctx context.Context) (workspace.Settings, error)
	// OpenItems are the items that are not done, cancelled, dropped, or promoted.
	OpenItems(ctx context.Context) ([]workspace.Item, error)
	PendingCandidates(ctx context.Context) (int, error)
	// AddReview records the daily review of the day, once.
	AddReview(ctx context.Context, due time.Time, pending int) error
	// Noticed is true when the item has a notice of the trigger.
	Noticed(ctx context.Context, item, trigger string) (bool, error)
	// AddNotice records one occurrence of a trigger, once.
	AddNotice(ctx context.Context, item, trigger string, at time.Time, reason string) error
	SaveItem(ctx context.Context, item workspace.Item) error
}

// Check records what is due for the owner at now: the daily review after its
// time, each trigger of a task that has come, and each shelved idea whose
// time condition has come or whose condition was found met.
func Check(ctx context.Context, u Unit, now time.Time) error {
	settings, err := u.Settings(ctx)
	if err != nil {
		return err
	}
	items, err := u.OpenItems(ctx)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return err
	}
	clock, err := time.Parse("15:04", settings.DailyReviewAt)
	if err != nil {
		return err
	}
	local := now.In(loc)
	if due := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, loc); !now.Before(due) {
		pending, err := u.PendingCandidates(ctx)
		if err != nil {
			return err
		}
		if err := u.AddReview(ctx, due, pending); err != nil {
			return err
		}
	}
	for _, item := range items {
		if item.Kind == "task" && settings.FollowUps {
			if err := fire(ctx, u, item, now); err != nil {
				return err
			}
		}
		if Wake(&item, settings, now) {
			if err := u.SaveItem(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

// fire records each trigger of a task that has come and whose guard holds.
// The start reminder of a task comes once, while the task waits to be done
// and its reminders are on.
func fire(ctx context.Context, u Unit, item workspace.Item, now time.Time) error {
	for _, trigger := range item.Triggers {
		if !trigger.Active || trigger.NextAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, trigger.NextAt)
		if err != nil || now.Before(at) {
			continue
		}
		if trigger.Guard != "" && trigger.Guard != item.Status {
			continue
		}
		if trigger.ID == effort.StartTrigger {
			if !item.RemindersOn || item.Status != "todo" && item.Status != "waiting" {
				continue
			}
			once, err := u.Noticed(ctx, item.ID, effort.StartTrigger)
			if err != nil {
				return err
			}
			if once {
				continue
			}
		}
		if err := u.AddNotice(ctx, item.ID, trigger.ID, at, trigger.Description); err != nil {
			return err
		}
	}
	return nil
}

// Wake wakes a shelved idea whose reminders are on when one of its
// conditions is met: a time condition by its time, another one when a
// source was found for it. A woken or snoozed idea stays as it is. It
// reports whether the idea changed.
func Wake(item *workspace.Item, settings workspace.Settings, now time.Time) bool {
	if item.Kind != "idea" || !settings.WakeIdeas || !item.RemindersOn || item.Status != "shelved" || item.Wake != nil {
		return false
	}
	stamp := now.UTC().Format(time.RFC3339)
	for i := range item.Conditions {
		condition := &item.Conditions[i]
		at, err := time.Parse(time.RFC3339, condition.DueAt)
		if condition.Kind == "time" && err == nil && !now.Before(at) {
			condition.Met = true
			condition.MetAt = stamp
		}
		if !condition.Met || condition.Kind != "time" && condition.MetBy == nil {
			continue
		}
		item.Status = "awakened"
		item.Wake = &workspace.Wake{At: stamp, Reason: condition.Description, ConditionID: condition.ID}
		item.Version++
		item.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
		item.Evolution = append(item.Evolution, workspace.Revision{At: item.UpdatedAt, By: "system", Summary: "唤醒：" + condition.Description})
		return true
	}
	return false
}

// PendingConditions are the event conditions that the extraction of a
// source looks for, of the ideas that may wake, latest first.
func PendingConditions(ideas []workspace.Item) []map[string]string {
	result := []map[string]string{}
	for _, item := range ideas {
		for _, c := range item.Conditions {
			if c.Kind == "event" && !c.Met && len(result) < pendingConditions {
				result = append(result, map[string]string{"idea_id": item.ID, "condition_id": c.ID, "idea": item.Title, "condition": c.Description})
			}
		}
	}
	return result
}

// SignalUnit is the storage of the signs that one source gives.
type SignalUnit interface {
	Item(ctx context.Context, id string) (workspace.Item, error)
	// AddSignalNotice records the notice of a sign once. added is false when
	// the condition already has a notice from the source.
	AddSignalNotice(ctx context.Context, item, condition string, at time.Time, reason string, source memory.Source) (added bool, err error)
	SaveItem(ctx context.Context, item workspace.Item) error
}

// ApplySignals wakes the ideas whose event condition a source gives a sign
// of. A sign wakes an idea for review; it does not confirm the condition.
// A sign without a quotation from the source, below the confidence, or for
// an idea that cannot wake is left out.
func ApplySignals(ctx context.Context, u SignalUnit, source memory.Source, signals []extraction.Signal, now time.Time) error {
	if len(signals) > signalLimit {
		return memory.ErrInvalid
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	for _, signal := range signals {
		if signal.Confidence < signalConfidence || signal.Confidence > 1 || signal.Quote == "" || !strings.Contains(source.Text, signal.Quote) || strings.TrimSpace(signal.Explanation) == "" || len(signal.Explanation) > explanationLength || !memory.ID(signal.IdeaID).Valid() {
			continue
		}
		item, err := u.Item(ctx, signal.IdeaID)
		if errors.Is(err, memory.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if item.Kind != "idea" || item.Status != "shelved" || !item.RemindersOn || item.Wake != nil {
			continue
		}
		for i := range item.Conditions {
			c := &item.Conditions[i]
			if c.ID != signal.ConditionID || c.Kind != "event" {
				continue
			}
			reason := "发现相关线索（待核验）：" + signal.Explanation
			added, err := u.AddSignalNotice(ctx, item.ID, c.ID, source.RecordedAt, reason, source)
			if err != nil {
				return err
			}
			if !added {
				continue
			}
			c.MetBy = &workspace.SourceRef{SourceID: string(source.ID), Version: source.Version, Label: source.Title, Excerpt: signal.Quote, At: stamp}
			item.Status = "awakened"
			item.Wake = &workspace.Wake{At: stamp, Reason: reason, ConditionID: c.ID}
			item.Evolution = append(item.Evolution, workspace.Revision{At: stamp, By: "ai", Summary: reason, SourceID: string(source.ID)})
			item.Version++
			item.UpdatedAt = stamp
			if err := u.SaveItem(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

// Suppressed are the triggers of a done task that an undo reopens and whose
// time passed more than an hour ago. Their occurrence is not sent again.
func Suppressed(current, restored workspace.Item, now time.Time) []workspace.Trigger {
	out := []workspace.Trigger{}
	if current.Kind != "task" || current.Status != "done" || restored.Status != "todo" && restored.Status != "doing" && restored.Status != "waiting" {
		return out
	}
	for _, trigger := range restored.Triggers {
		if !trigger.Active || trigger.NextAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, trigger.NextAt)
		if err != nil || !at.Before(now.Add(-restoredGrace)) {
			continue
		}
		out = append(out, trigger)
	}
	return out
}
