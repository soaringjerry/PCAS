// Package schedule owns the owner's schedule view: which remembered dates
// and task dates fall on which day of a range, the repeats of a recurring
// arrangement, what is unclear, and what is overdue. Storage reads the
// rows.
package schedule

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// RangeDays is the longest range of one schedule read.
const RangeDays = 62

// InProgressLimit is the count of undated tasks that the home page lists.
const InProgressLimit = 12

// Window is the range of a query in the owner's time zone: its first and
// last day, and the instant after its last day.
func Window(q workspace.ScheduleQuery, loc *time.Location) (from, to, end time.Time, err error) {
	from, err = time.Parse("2006-01-02", q.From)
	if err != nil {
		return from, to, end, memory.ErrInvalid
	}
	to, err = time.Parse("2006-01-02", q.To)
	if err != nil || to.Before(from) || !to.Before(from.AddDate(0, 0, RangeDays)) {
		return from, to, end, memory.ErrInvalid
	}
	next := to.AddDate(0, 0, 1)
	return from, to, time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc), nil
}

// Deadline is a remembered date as storage reads it.
type Deadline struct {
	Entry      workspace.ScheduleEntry
	At         *time.Time
	Recurrence string
	Rule       json.RawMessage
}

// Build places the remembered dates and the dated tasks of the owner on
// the days of the range. A recurring arrangement repeats by its rule; one
// whose rule is not clear yet, or whose clock time a daylight-saving change
// skips, is unclear. A date before the range is overdue only for a deadline
// or a task's due date. A task due at 23:59 without a stated time is a date.
func Build(q workspace.ScheduleQuery, timezone string, deadlines []Deadline, tasks []workspace.Item) (workspace.Schedule, error) {
	out := workspace.Schedule{From: q.From, To: q.To, Timezone: timezone, Days: []workspace.ScheduleDay{}, Unclear: []workspace.ScheduleEntry{}, Overdue: []workspace.ScheduleEntry{}}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return out, err
	}
	from, to, _, err := Window(q, loc)
	if err != nil {
		return out, err
	}
	rangeStart := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	days := map[string]int{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		days[date] = len(out.Days)
		out.Days = append(out.Days, workspace.ScheduleDay{Date: date, Items: []workspace.ScheduleEntry{}})
	}
	add := func(entry workspace.ScheduleEntry) {
		if i, ok := days[entry.Date]; ok {
			out.Days[i].Items = append(out.Days[i].Items, entry)
		}
	}
	for _, d := range deadlines {
		e := d.Entry
		if e.Kind == "recurring" {
			var rule organize.ScheduleRule
			valid := len(d.Rule) > 0 && json.Unmarshal(d.Rule, &rule) == nil && rule.Valid()
			if !valid {
				rule, valid = organize.LegacyRule(d.Recurrence)
			}
			if !valid {
				e.At = nil
				e.DateOnly = true
				e.TimeNote = "周期尚未明确，正在整理；" + e.TimeNote
				out.Unclear = append(out.Unclear, e)
				continue
			}
			for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
				if !rule.Occurs(day) {
					continue
				}
				instance := e
				instance.Date = day.Format("2006-01-02")
				instance.ID = e.ID + ":" + instance.Date
				instance.At = nil
				instance.DateOnly = rule.Clock == ""
				if rule.Clock != "" {
					when, ok := organize.Clock(day, rule.Clock, loc)
					if !ok {
						instance.TimeNote = "当地夏令时跳过了这个钟点；" + e.TimeNote
						out.Unclear = append(out.Unclear, instance)
						continue
					}
					v := when.Format(time.RFC3339)
					instance.At = &v
				} else if instance.TimeNote == "" {
					instance.TimeNote = "未说明具体时间"
				}
				add(instance)
			}
			continue
		}
		if d.At == nil || e.Kind == "unclear" {
			e.At = nil
			e.DateOnly = true
			out.Unclear = append(out.Unclear, e)
			continue
		}
		local := d.At.In(loc)
		e.Date = local.Format("2006-01-02")
		if e.DateOnly {
			e.At = nil
		} else {
			v := local.Format(time.RFC3339)
			e.At = &v
		}
		if local.Before(rangeStart) {
			if e.Kind == "deadline" {
				out.Overdue = append(out.Overdue, e)
			}
			continue
		}
		add(e)
	}
	for _, item := range tasks {
		for _, v := range []struct{ kind, value string }{{"due", item.Due}, {"scheduled", item.Scheduled}} {
			if v.value == "" || v.kind == "scheduled" && v.value == item.Due {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, v.value)
			dateOnly := false
			if err != nil {
				at, err = time.ParseInLocation("2006-01-02", v.value, loc)
				dateOnly = err == nil
			}
			if err != nil {
				continue
			}
			local := at.In(loc)
			if v.kind == "due" {
				if item.DueDateOnly != nil {
					dateOnly = *item.DueDateOnly
				} else {
					dateOnly = dateOnly || local.Hour() == 23 && local.Minute() == 59 && local.Second() == 0
				}
			}
			e := workspace.ScheduleEntry{ID: item.ID + ":" + v.kind, Title: item.Title, Kind: "task", Date: local.Format("2006-01-02"), DateOnly: dateOnly, Source: workspace.ScheduleSource{Kind: "task", ItemID: item.ID}}
			if item.Creation != nil && item.Creation.Source != nil {
				e.OriginalText = item.Creation.Source.Excerpt
			}
			if dateOnly {
				e.TimeNote = "只有日期"
			} else {
				timeValue := local.Format(time.RFC3339)
				e.At = &timeValue
			}
			if local.Before(rangeStart) {
				if v.kind == "due" {
					out.Overdue = append(out.Overdue, e)
				}
				continue
			}
			add(e)
		}
	}
	for i := range out.Days {
		sort.Slice(out.Days[i].Items, func(a, b int) bool { return before(out.Days[i].Items[a], out.Days[i].Items[b]) })
	}
	sort.Slice(out.Overdue, func(a, b int) bool { return before(out.Overdue[a], out.Overdue[b]) })
	return out, nil
}

// before orders entries by their time; an entry without a time comes after
// those with one, then by date and identity.
func before(a, b workspace.ScheduleEntry) bool {
	if a.At == nil || b.At == nil {
		if a.At == nil && b.At != nil {
			return false
		}
		if a.At != nil && b.At == nil {
			return true
		}
	} else {
		ta, _ := time.Parse(time.RFC3339, *a.At)
		tb, _ := time.Parse(time.RFC3339, *b.At)
		if !ta.Equal(tb) {
			return ta.Before(tb)
		}
	}
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return a.ID < b.ID
}

// InProgress are the open tasks without a date and not urgent, the latest
// first, at most InProgressLimit with the count of the rest.
func InProgress(tasks []workspace.Item) workspace.InProgress {
	out := workspace.InProgress{Items: []workspace.Item{}}
	for _, item := range tasks {
		if strings.TrimSpace(item.Due) != "" || strings.TrimSpace(item.Scheduled) != "" || item.Urgent {
			continue
		}
		out.Total++
		if len(out.Items) < InProgressLimit {
			out.Items = append(out.Items, item)
		}
	}
	out.Remaining = out.Total - len(out.Items)
	return out
}
