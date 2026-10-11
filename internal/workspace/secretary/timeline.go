package secretary

import (
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func currentInput(connector string) bool {
	switch connector {
	case "desk", "capture", "telegram", "desk-incomplete":
		return true
	}
	return false
}

// TimelineTask is a task that the turn of a memory made.
type TimelineTask struct {
	ID     string
	Status string
}

// TimelineSource is the source of a memory on the owner's timeline card.
type TimelineSource struct {
	workspace.SourceRef
	ExpressedAt string
	Connector   string
}

// TimelineAt is the time of a memory on the timeline: when it was said, else
// when its source was said, else the record time of the owner's own input.
// The record time of an import is not its date.
func TimelineAt(expressed string, source TimelineSource) *string {
	if expressed != "" {
		return &expressed
	}
	if source.ExpressedAt != "" {
		return &source.ExpressedAt
	}
	if currentInput(source.Connector) {
		return &source.At
	}
	return nil
}

// TimelineStatus is "done" or "dropped" when each task of the memory is, and
// "changed" for a memory without tasks that was changed.
func TimelineStatus(tasks []TimelineTask, changed bool) string {
	if len(tasks) == 0 {
		if changed {
			return "changed"
		}
		return "open"
	}
	done, dropped := true, true
	for _, task := range tasks {
		done = done && task.Status == "done"
		dropped = dropped && (task.Status == "cancelled" || task.Status == "dropped")
	}
	if done {
		return "done"
	}
	if dropped {
		return "dropped"
	}
	return "open"
}

// TimelineVisible reports whether a timeline card is shown: two items, or
// one dated item when the owner asked to recall.
func TimelineVisible(items []workspace.DeskTimelineItem, recall bool) bool {
	if len(items) >= 2 {
		return true
	}
	return recall && len(items) == 1 && items[0].At != nil
}

// TimelineBefore orders items by time. An item without a time is last.
func TimelineBefore(a, b workspace.DeskTimelineItem) bool {
	if a.At == nil {
		return false
	}
	if b.At == nil {
		return true
	}
	left, _ := time.Parse(time.RFC3339Nano, *a.At)
	right, _ := time.Parse(time.RFC3339Nano, *b.At)
	return left.Before(right)
}
