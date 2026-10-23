package organize

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// ApplyUnit is the storage of the result of one organize batch, in one
// transaction under the owner lock and the job lease. Each method reads or
// writes; none decides.
type ApplyUnit interface {
	// Current locks the statement and is true when it is still the version
	// that was organized and still needs this rule version.
	Current(ctx context.Context, ref memory.Ref, version int) (bool, error)
	// Groups resolves the groups of each current statement, making the
	// declared groups that a current statement uses.
	Groups(ctx context.Context, items map[int]Item, declared []Group, current map[int]bool) (map[int][]Group, error)
	// Defer counts one more failed attempt of the statement and makes it
	// wait before the next.
	Defer(ctx context.Context, id memory.ID) error
	// Count records an outcome of the stage by reason.
	Count(ctx context.Context, outcome, reason string, n int) error
	// Label writes the category and the durability of the statement version.
	Label(ctx context.Context, ref memory.Ref, category string, durable *bool) error
	// SetGroups replaces the project, topic, and area groups of the statement.
	SetGroups(ctx context.Context, ref memory.Ref, groups []Group) error
	// Dates replaces the dates of the statement and returns how many earlier
	// dates are gone.
	Dates(ctx context.Context, m Memory, deadlines []Deadline, timezone string) (withdrawn int, err error)
	// Requirement removes the statement's requirement and, for a rule, writes
	// it again with its scope. removed is the count of rows removed.
	Requirement(ctx context.Context, ref memory.Ref, rule bool, unrestricted bool, scope string) (removed int, err error)
	// Organized marks the statement organized under the rule version.
	Organized(ctx context.Context, id memory.ID, version int) error
	// Finish acknowledges the job, removes its paid result, and queues the
	// next batch.
	Finish(ctx context.Context) error
}

// Apply writes the result of one batch. A statement that changed since it
// was read is left for its next turn. A statement without a valid answer
// waits and is counted. Each other statement gets its category, its groups,
// its dates, and, for a rule, its requirement; dates and requirements that
// a new answer no longer gives are counted as withdrawn.
func Apply(ctx context.Context, u ApplyUnit, result Result) (Applied, error) {
	deferred := []memory.ID{}
	current := map[int]bool{}
	for _, m := range result.Memories {
		ok, err := u.Current(ctx, m.Ref, result.Version)
		if err != nil {
			return Applied{}, err
		}
		if ok {
			current[m.N] = true
		}
	}
	groups, err := u.Groups(ctx, result.Items, UsedDeclarations(result.Declared, result.Items, current), current)
	if err != nil {
		return Applied{}, err
	}
	for _, m := range result.Memories {
		if !current[m.N] {
			continue
		}
		item, valid := result.Items[m.N]
		if !valid {
			if err := u.Defer(ctx, m.Ref.ID); err != nil {
				return Applied{}, err
			}
			deferred = append(deferred, m.Ref.ID)
			if err := u.Count(ctx, "failure", "organize_invalid_output", 1); err != nil {
				return Applied{}, err
			}
			continue
		}
		if err := u.Label(ctx, m.Ref, item.Category, item.Durable); err != nil {
			return Applied{}, err
		}
		if err := u.SetGroups(ctx, m.Ref, groups[m.N]); err != nil {
			return Applied{}, err
		}
		deadlines := append([]Deadline{}, item.Deadlines...)
		for i := range deadlines {
			deadlines[i].N = 1
		}
		withdrawn, err := u.Dates(ctx, m, deadlines, result.Timezone)
		if err != nil {
			return Applied{}, err
		}
		if withdrawn > 0 {
			if err := u.Count(ctx, "success", "reorganize_deadlines_withdrawn", withdrawn); err != nil {
				return Applied{}, err
			}
		}
		removed, err := u.Requirement(ctx, m.Ref, item.Category == "rule", item.Unrestricted, item.Scope)
		if err != nil {
			return Applied{}, err
		}
		if item.Category != "rule" && removed > 0 {
			if err := u.Count(ctx, "success", "reorganize_requirements_withdrawn", removed); err != nil {
				return Applied{}, err
			}
		}
		if err := u.Organized(ctx, m.Ref.ID, result.Version); err != nil {
			return Applied{}, err
		}
	}
	if err := u.Finish(ctx); err != nil {
		return Applied{}, err
	}
	return Applied{Deferred: deferred}, nil
}
