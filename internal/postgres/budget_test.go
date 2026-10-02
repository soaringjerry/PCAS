package postgres

import (
	"testing"
	"time"
)

func TestNextBudgetDayUsesLocalCalendar(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		month    time.Month
		day      int
		duration time.Duration
	}{
		{time.October, 4, 23 * time.Hour}, {time.April, 5, 25 * time.Hour},
	} {
		start := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, loc)
		next := nextBudgetDay(start, loc)
		if next.Sub(start) != tc.duration || next.In(loc).Hour() != 0 {
			t.Fatalf("next=%v duration=%v", next, next.Sub(start))
		}
	}
	utc := time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC)
	if got := nextBudgetDay(utc, time.UTC); !got.Equal(time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(got)
	}
}

func TestRetryDelayMatchesBoundedSchedule(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for i, v := range want {
		if got := retryDelay(i + 1); got != v {
			t.Fatalf("attempt %d: %v", i+1, got)
		}
	}
}

func TestNextBudgetDayAtMidnightDSTJump(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		month time.Month
		day   int
		hour  int
	}{
		{"America/Santiago", time.September, 6, 1},
		{"America/Havana", time.November, 1, 0},
	} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, tc.month, tc.day-1, 12, 0, 0, 0, loc)
		next := nextBudgetDay(now, loc)
		if next.In(loc).Day() != tc.day || next.In(loc).Hour() != tc.hour || next.Add(-time.Nanosecond).In(loc).Day() == tc.day {
			t.Fatalf("%s next=%v preceding=%v", tc.zone, next, next.Add(-time.Nanosecond).In(loc))
		}
	}
}
