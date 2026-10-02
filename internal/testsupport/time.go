// Package testsupport contains fixtures shared by integration tests.
package testsupport

import (
	"sync"
	"testing"
	"time"
)

var testDates sync.Map

// DateFromToday fixes one calendar anchor per test, including across midnight,
// then applies calendar days and a wall-clock time in the test's explicit zone.
func DateFromToday(t testing.TB, zone string, days, hour, minute int) time.Time {
	t.Helper()
	anchor, loaded := testDates.LoadOrStore(t, time.Now())
	if !loaded {
		t.Cleanup(func() { testDates.Delete(t) })
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	date := anchor.(time.Time).In(loc).AddDate(0, 0, days)
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
}
