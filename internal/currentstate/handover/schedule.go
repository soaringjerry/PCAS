package handover

import (
	"context"
	"time"
)

// Written is the owner's handover as the scheduler sees it.
type Written struct {
	// Hash is the hash of the current input.
	Hash string
	// InputHash is the hash of the input the handover was written from under
	// this rule; nil when there is none.
	InputHash *string
	BuiltAt   *time.Time
	// Pending is true while a handover job waits or runs.
	Pending bool
}

// ScheduleUnit reads and queues the handover of one owner. Each method reads
// or writes; none decides.
type ScheduleUnit interface {
	Written(ctx context.Context) (Written, error)
	// HasMembers is true when some memory belongs in a handover.
	HasMembers(ctx context.Context) (bool, error)
	Queue(ctx context.Context, due time.Time) error
}

// Schedule queues a new handover when its input changed: not while one is
// being written, the first only when some memory belongs in it, and an hour
// after the last at the earliest. It is true when one was queued.
func Schedule(ctx context.Context, u ScheduleUnit, now time.Time) (bool, error) {
	w, err := u.Written(ctx)
	if err != nil || w.Pending || w.InputHash != nil && *w.InputHash == w.Hash {
		return false, err
	}
	if w.InputHash == nil {
		members, err := u.HasMembers(ctx)
		if err != nil || !members {
			return false, err
		}
	}
	due := now
	if w.BuiltAt != nil && w.BuiltAt.Add(MinimumInterval).After(due) {
		due = w.BuiltAt.Add(MinimumInterval)
	}
	return true, u.Queue(ctx, due)
}
