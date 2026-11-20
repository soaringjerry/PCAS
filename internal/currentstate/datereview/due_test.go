package datereview

import (
	"testing"
	"time"
)

func TestADateIsReviewedWhenItIsNoTimeToShow(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	yesterday, lastWeek, tomorrow := now.Add(-20*time.Hour), now.Add(-7*24*time.Hour), now.Add(24*time.Hour)
	for _, c := range []struct {
		kind, clock string
		at          *time.Time
		class       string
		ok          bool
	}{
		{"unclear", "", nil, "unclear", true},
		{"recurring", "", nil, "habit", true},
		{"recurring", "09:00", nil, "habit", false},
		{"deadline", "", &lastWeek, "past", true},
		{"appointment", "", &lastWeek, "past", true},
		{"deadline", "", &yesterday, "past", false},
		{"deadline", "", &tomorrow, "past", false},
		{"deadline", "", nil, "past", false},
		{"other", "", &lastWeek, "", false},
	} {
		class, ok := Class(c.kind, c.at, c.clock, now)
		if ok != c.ok || ok && class != c.class {
			t.Errorf("%s %q %v: class = %q, ok = %v", c.kind, c.clock, c.at, class, ok)
		}
	}
}

func TestOneDatePerMemoryIsReviewedOncePerVersionAndClass(t *testing.T) {
	now := time.Now()
	soon := now.Add(time.Hour)
	rows := []Open{
		{Claim: "a", Version: 1, Kind: "deadline", At: &soon, Entry: Entry{Title: "not yet"}},
		{Claim: "a", Version: 1, Kind: "unclear", Entry: Entry{Title: "first"}},
		{Claim: "a", Version: 1, Kind: "unclear", Entry: Entry{Title: "second"}},
		{Claim: "b", Version: 2, Kind: "unclear", Judged: []string{"unclear"}},
		{Claim: "b", Version: 2, Kind: "recurring", Entry: Entry{Title: "habit"}},
		{Claim: "c", Version: 1, Kind: "recurring", Clock: "08:00"},
	}
	due := Pick(rows, now)
	if len(due) != 2 || due[0].Claim != "a" || due[0].Entry.Title != "first" || due[0].Class != "unclear" || due[1].Claim != "b" || due[1].Class != "habit" || due[1].Version != 2 {
		t.Fatalf("due = %+v", due)
	}
}
