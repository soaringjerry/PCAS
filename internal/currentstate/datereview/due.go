package datereview

import (
	"slices"
	"time"
)

// Open is a row of an open date of a current memory, with the classes that
// a review already judged for this memory version.
type Open struct {
	Claim   string
	Version int
	Kind    string
	At      *time.Time
	// Clock is the hour of a recurring arrangement.
	Clock  string
	Judged []string
	Entry  Entry
}

// Due is a date to review.
type Due struct {
	Claim   string
	Version int
	Class   string
	Entry   Entry
}

// Class is what a review judges of a date: "unclear" for one never pinned
// down, "habit" for a fixed arrangement with no hour, "past" for a dated row
// over for PastAfter. ok is false for a date that is a time to show.
func Class(kind string, at *time.Time, clock string, now time.Time) (class string, ok bool) {
	switch kind {
	case "unclear":
		return "unclear", true
	case "recurring":
		return "habit", clock == ""
	case "deadline", "appointment":
		return "past", at != nil && at.Before(now.Add(-PastAfter))
	}
	return "", false
}

// Pick is one date per memory to review, in the rows' order: the memory's
// first row that a review judges and has not judged in its class for this
// memory version.
func Pick(rows []Open, now time.Time) []Due {
	out := []Due{}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Claim] {
			continue
		}
		class, ok := Class(r.Kind, r.At, r.Clock, now)
		if !ok || slices.Contains(r.Judged, class) {
			continue
		}
		seen[r.Claim] = true
		out = append(out, Due{Claim: r.Claim, Version: r.Version, Class: class, Entry: r.Entry})
	}
	return out
}
