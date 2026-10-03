package postgres

import (
	"testing"
	"time"
)

// A date without a time means the end of that day where the user is, also on
// the days the clocks change.
func TestDateOnlyDueStaysOnItsDayAcrossClockChanges(t *testing.T) {
	year := time.Now().Year() + 1
	for _, zone := range []string{"Australia/Melbourne", "Europe/Berlin", "America/New_York", "Asia/Shanghai"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		for day := time.Date(year, 1, 1, 12, 0, 0, 0, loc); day.Year() == year; day = day.AddDate(0, 0, 1) {
			value := day.Format("2006-01-02")
			out, dateOnly, ok := deskDue(value, loc)
			at, err := time.Parse(time.RFC3339, out)
			if !ok || !dateOnly || err != nil {
				t.Fatalf("%s %s: %q %v %v %v", zone, value, out, dateOnly, ok, err)
			}
			if local := at.In(loc); local.Format("2006-01-02 15:04") != value+" 23:59" {
				t.Fatalf("%s %s became %s", zone, value, local.Format("2006-01-02 15:04"))
			}
		}
	}
}
