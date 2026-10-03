package postgres

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestUsageDatesCalendarBoundaries(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		day   string
		hours time.Duration
	}{
		{"2026-10-04", 23}, {"2026-04-05", 25}, {"2026-09-30", 24},
	} {
		start, end, err := usageDates(tc.day, tc.day, loc, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if end.Sub(start) != tc.hours*time.Hour {
			t.Fatalf("%s: duration %s", tc.day, end.Sub(start))
		}
		if start.Hour() != 0 || end.Hour() != 0 || start.Format(time.DateOnly) != tc.day {
			t.Fatalf("incorrect boundaries: %s %s", start, end)
		}
	}
	now := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	start, end, err := usageDates("", "", loc, now)
	if err != nil || start.Format(time.DateOnly) != "2026-09-03" || end.Format(time.DateOnly) != "2026-10-03" {
		t.Fatalf("defaults: %s %s %v", start, end, err)
	}
	for _, tc := range [][2]string{{"2026-02-30", ""}, {"", "bad"}, {"2026-10-03", "2026-10-02"}} {
		if _, _, err := usageDates(tc[0], tc[1], loc, now); !errors.Is(err, memory.ErrInvalid) {
			t.Fatalf("accepted invalid dates: %v", tc)
		}
	}
}

func TestUsageCursorValidation(t *testing.T) {
	want := usageCursor{At: time.Now().UTC().Truncate(time.Microsecond), ID: memory.NewID()}
	raw := base64.RawURLEncoding.EncodeToString(asJSON(want))
	got, err := parseUsageCursor(raw)
	if err != nil || got != want {
		t.Fatalf("cursor round trip: %v %v", got, err)
	}
	for _, raw := range []string{"invalid!", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"at":"2026-10-02T00:00:00Z","id":"bad"}`))} {
		if _, err := parseUsageCursor(raw); !errors.Is(err, memory.ErrInvalid) {
			t.Fatalf("accepted cursor %q", raw)
		}
	}
}
