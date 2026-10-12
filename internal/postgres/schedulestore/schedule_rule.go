package schedulestore

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
)

type ScheduleRule = organize.ScheduleRule

// Compatibility decodes only complete, fixed calendar expressions already
// stored by old models. It never inspects titles/originals or guesses meaning.
var legacyScheduleGrammar = regexp.MustCompile(`^每(天|日|周[一二三四五六日天]|星期[一二三四五六日天]|月[0-9]{1,2}[日号])(?:\s*([0-9]{1,2}):([0-9]{2}))?$`)

func LegacyScheduleRule(raw string, expressed time.Time, loc *time.Location) (ScheduleRule, bool) {
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

// A fold occurs once, using the earliest matching instant. A DST gap has no
// occurrence and is returned as an unresolved entry, retaining its source.
func scheduleClock(day time.Time, clock string, loc *time.Location) (time.Time, bool) {
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
