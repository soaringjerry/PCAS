// Package organize owns the memory interpretation rules that label one
// statement: its category, its groups, its dates, and the scope of a rule.
// It also owns the flow of one organize job. The rules here need no storage.
package organize

import "time"

// ScheduleRule is a normalized calendar grammar, supplied by the categorization model.
// Clock is local civil HH:MM; an absent clock never means midnight.
type ScheduleRule struct {
	Frequency string `json:"frequency"`
	Interval  int    `json:"interval"`
	Anchor    string `json:"anchor"`
	Weekdays  []int  `json:"weekdays"`
	MonthDays []int  `json:"monthDays"`
	Clock     string `json:"clock"`
	Until     string `json:"until"`
}

// Valid reports whether the rule is a complete calendar expression.
func (r ScheduleRule) Valid() bool {
	if !oneOf(r.Frequency, "daily", "weekly", "monthly", "yearly") || r.Interval < 1 {
		return false
	}
	if r.Interval > 1 && r.Anchor == "0001-01-01" {
		return false
	}
	if _, err := time.Parse("2006-01-02", r.Anchor); err != nil {
		return false
	}
	if r.Clock != "" {
		if _, err := time.Parse("15:04", r.Clock); err != nil {
			return false
		}
	}
	if r.Until != "" {
		if _, err := time.Parse("2006-01-02", r.Until); err != nil || r.Until < r.Anchor {
			return false
		}
	}
	for _, n := range r.Weekdays {
		if n < 1 || n > 7 {
			return false
		}
	}
	for _, n := range r.MonthDays {
		if n == 0 || n < -31 || n > 31 {
			return false
		}
	}
	return true
}

// Occurs reports whether a valid rule has an occurrence on the civil day.
func (r ScheduleRule) Occurs(day time.Time) bool {
	date := day.Format("2006-01-02")
	if date < r.Anchor || r.Until != "" && date > r.Until {
		return false
	}
	anchor, _ := time.Parse("2006-01-02", r.Anchor)
	civil := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	days := 0
	// Time.Sub saturates after ~290 years; Unix seconds handles all civil years.
	days = int((civil.Unix() - anchor.Unix()) / 86400)
	contains := func(ns []int, n int) bool {
		for _, x := range ns {
			if x == n {
				return true
			}
		}
		return false
	}
	weekday := (int(day.Weekday())+6)%7 + 1
	switch r.Frequency {
	case "daily":
		return days%r.Interval == 0 && (len(r.Weekdays) == 0 || contains(r.Weekdays, weekday))
	case "weekly":
		anchorWeekday := (int(anchor.Weekday())+6)%7 + 1
		weeks := (days + anchorWeekday - 1) / 7
		return weeks%r.Interval == 0 && (len(r.Weekdays) > 0 && contains(r.Weekdays, weekday) || len(r.Weekdays) == 0 && weekday == anchorWeekday)
	case "monthly":
		months := (day.Year()-anchor.Year())*12 + int(day.Month()-anchor.Month())
		lastDay := time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		match := false
		for _, n := range r.MonthDays {
			if n < 0 {
				n = lastDay + n + 1
			}
			match = match || day.Day() == n
		}
		return months%r.Interval == 0 && (len(r.MonthDays) > 0 && match || len(r.MonthDays) == 0 && day.Day() == anchor.Day())
	case "yearly":
		return (day.Year()-anchor.Year())%r.Interval == 0 && day.Month() == anchor.Month() && day.Day() == anchor.Day()
	}
	return false
}

// Deadline is one date that the model reads from a statement.
type Deadline struct {
	ScheduleRule *ScheduleRule `json:"scheduleRule,omitempty"`
	DateOnly     *bool         `json:"dateOnly,omitempty"`
	N            int           `json:"n"`
	Kind         string        `json:"kind"`
	At           *string       `json:"at"`
	Recurrence   string        `json:"recurrence"`
	Title        string        `json:"title"`
	TimeNote     string        `json:"timeNote"`
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
