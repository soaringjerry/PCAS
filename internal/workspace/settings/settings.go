// Package settings owns the workspace rules for a change of the owner's
// settings and of an agent's settings. The rules need no storage.
package settings

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
)

// cityLimit is the longest name of a city, in characters. The city is one
// line of each secretary input.
const cityLimit = 60

// budgetLimit is the largest daily budget that can be set.
const budgetLimit = 1e6

// CheckZone returns workspace.ErrTimezone when a patch names a time zone
// that is not valid. The caller can make this check before it reads.
func CheckZone(patch json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(patch, &fields) == nil {
		if raw, present := fields["timezone"]; present {
			var zone string
			if json.Unmarshal(raw, &zone) != nil || workspace.ValidateTimezone(zone) != nil {
				return workspace.ErrTimezone
			}
		}
	}
	return nil
}

// Apply returns the owner's settings after a patch. replan is true when the
// patch changes the time zone or the time of the daily review: the start
// days of the tasks then need a new calculation.
// A time zone that is not valid returns workspace.ErrTimezone. The retired
// field autoAccept is ignored and is always false.
func Apply(current workspace.Settings, patch json.RawMessage) (next workspace.Settings, replan bool, err error) {
	if err := CheckZone(patch); err != nil {
		return current, false, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(patch, &fields) != nil || len(fields) == 0 {
		return current, false, memory.ErrInvalid
	}
	delete(fields, "autoAccept")
	next = current
	if len(fields) != 0 {
		rest, _ := json.Marshal(fields)
		if err := itemcommand.Patch(&next, rest, "wakeIdeas", "followUps", "dailyReviewAt", "dailyBudget", "timezone", "city"); err != nil {
			return current, false, err
		}
	}
	next.AutoAccept = false
	next.City = strings.TrimSpace(next.City)
	if utf8.RuneCountInString(next.City) > cityLimit || strings.ContainsAny(next.City, "\r\n") {
		return current, false, memory.ErrInvalid
	}
	if next.DailyBudget < 0 || next.DailyBudget > budgetLimit {
		return current, false, memory.ErrInvalid
	}
	if _, err := time.Parse("15:04", next.DailyReviewAt); err != nil {
		return current, false, memory.ErrInvalid
	}
	if err := workspace.ValidateTimezone(next.Timezone); err != nil {
		return current, false, err
	}
	_, zone := fields["timezone"]
	_, clock := fields["dailyReviewAt"]
	return next, zone || clock, nil
}

// ApplyAgent returns an agent after a patch. initialize is true when the
// agent is enabled for the first time: its memory selection is then made
// once, and the caller sets MemoryInitialized after it.
func ApplyAgent(current workspace.Agent, patch json.RawMessage) (next workspace.Agent, initialize bool, err error) {
	next = current
	if err := itemcommand.Patch(&next, patch, "enabled", "memoryKinds", "includeInferred", "note", "name"); err != nil {
		return current, false, err
	}
	if strings.TrimSpace(next.Name) == "" || len(next.Name) > 1<<20 {
		return current, false, memory.ErrInvalid
	}
	for _, kind := range next.MemoryKinds {
		switch kind {
		case "fact", "preference", "decision", "intention", "plan":
		default:
			return current, false, memory.ErrInvalid
		}
	}
	return next, next.Enabled && !next.MemoryInitialized, nil
}
