// Package organize owns the memory interpretation rules that label one
// statement: its category, its groups, its dates, and the scope of a rule.
// It also owns the flow of one organize job. The rules here need no storage.
package organize

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

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

// Compatibility decodes only complete, fixed calendar expressions already
// stored by old models. It never inspects titles/originals or guesses meaning.
var legacyScheduleGrammar = regexp.MustCompile(`^每(天|日|周[一二三四五六日天]|星期[一二三四五六日天]|月[0-9]{1,2}[日号])(?:\s*([0-9]{1,2}):([0-9]{2}))?$`)

// LegacyRule reads a recurrence that an old model stored as words.
func LegacyRule(raw string) (ScheduleRule, bool) {
	r := ScheduleRule{Interval: 1, Anchor: "0001-01-01"}
	p := legacyScheduleGrammar.FindStringSubmatch(strings.TrimSpace(raw))
	if p == nil {
		return r, false
	}
	switch {
	case p[1] == "天" || p[1] == "日":
		r.Frequency = "daily"
	case strings.HasPrefix(p[1], "周") || strings.HasPrefix(p[1], "星期"):
		r.Frequency = "weekly"
		last := []rune(p[1])
		n := strings.IndexRune("一二三四五六日", last[len(last)-1])
		if last[len(last)-1] == '天' {
			n = 18
		}
		r.Weekdays = []int{n/3 + 1}
	default:
		r.Frequency = "monthly"
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(p[1], "月"), "日"), "号"))
		if err != nil {
			return r, false
		}
		r.MonthDays = []int{n}
	}
	if p[2] != "" {
		h, err := strconv.Atoi(p[2])
		if err != nil {
			return r, false
		}
		r.Clock = strconv.Itoa(h/10) + strconv.Itoa(h%10) + ":" + p[3]
	}
	return r, r.Valid()
}

// Clock is the instant of the clock time on the day. A fold occurs once, at
// the earliest matching instant. A DST gap has no
// occurrence and is returned as an unresolved entry, retaining its source.
func Clock(day time.Time, clock string, loc *time.Location) (time.Time, bool) {
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, false
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	valid := func(x time.Time) bool {
		return x.Year() == day.Year() && x.Month() == day.Month() && x.Day() == day.Day() && x.Hour() == t.Hour() && x.Minute() == t.Minute()
	}
	if !valid(at) {
		return time.Time{}, false
	}
	// Adjacent civil-day offsets cover folds without assuming their size.
	_, offset := at.Zone()
	for _, neighbor := range []time.Time{at.AddDate(0, 0, -1), at.AddDate(0, 0, 1)} {
		_, otherOffset := neighbor.Zone()
		other := at.Add(time.Duration(offset-otherOffset) * time.Second)
		if valid(other) && other.Before(at) {
			at = other
		}
	}

	return at, true
}

// NormalizeDeadlines checks the dates that the model gave a memory. A kind
// it does not know, or a date without a title, makes the answer invalid. A
// recurring arrangement without a stated repeat, and a date that cannot be
// read or lies before the memory was said, stay as unclear with their
// original words and a note why. A date at midnight with a time note is a
// date without a time.
func NormalizeDeadlines(output []Deadline, expressedAt string, loc *time.Location) ([]Deadline, error) {
	normalized := []Deadline{}
	for _, d := range output {
		if !oneOf(d.Kind, "deadline", "appointment", "recurring", "unclear") || strings.TrimSpace(d.Title) == "" {
			return nil, memory.ErrInvalid
		}
		switch d.Kind {
		case "recurring":
			d.At = nil
			if d.ScheduleRule != nil && !d.ScheduleRule.Valid() {
				return nil, memory.ErrInvalid
			}
			if d.ScheduleRule == nil {
				if rule, ok := LegacyRule(d.Recurrence); ok {
					d.ScheduleRule = &rule
				}
			}
			if strings.TrimSpace(d.Recurrence) == "" {
				d.Kind = "unclear"
				d.TimeNote = "未说明周期；" + d.TimeNote
			}
		case "unclear":
			d.At = nil
		default:
			valid := false
			if d.At != nil {
				at, e := time.Parse(time.RFC3339Nano, *d.At)
				if e != nil {
					at, e = time.ParseInLocation("2006-01-02", *d.At, loc)
				}
				said, se := time.Parse(time.RFC3339Nano, expressedAt)
				if e == nil && (se != nil || !at.Before(said)) {
					v := at.UTC().Format(time.RFC3339Nano)
					d.At = &v
					valid = true
				}
			}
			if !valid {
				d.Kind = "unclear"
				d.At = nil
				d.TimeNote = "日期无法校验；" + d.TimeNote
			}
		}
		if d.DateOnly == nil && d.At != nil {
			at, e := time.Parse(time.RFC3339Nano, *d.At)
			only := e == nil && at.In(loc).Hour() == 0 && at.In(loc).Minute() == 0 && d.TimeNote != ""
			d.DateOnly = &only
		}
		normalized = append(normalized, d)
	}
	return normalized, nil
}
