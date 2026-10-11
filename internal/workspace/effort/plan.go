// Package effort owns the workspace rules for task effort: the start date that
// follows from an estimate, and the estimation of tasks that have none.
// Storage supplies the transactions. It does not decide which task changes.
package effort

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// DailyHours is the number of work hours that one day supplies to a plan.
const DailyHours = 4.0

// StartTrigger identifies the reminder that the plan keeps on a task.
const StartTrigger = "start-work"

// StartDate gives the day on which work must start to finish before the due
// time. It counts back whole workdays and does not count Saturday or Sunday.
// A task without an estimate or a due time has no start date.
func StartDate(due string, hours *float64, loc *time.Location) (*string, error) {
	if hours != nil && (math.IsNaN(*hours) || math.IsInf(*hours, 0) || *hours < 0 || math.Ceil(*hours/DailyHours) > 366*9999) {
		return nil, memory.ErrInvalid
	}
	if hours == nil || due == "" {
		return nil, nil
	}
	end, err := time.Parse(time.RFC3339, due)
	if err != nil {
		return nil, memory.ErrInvalid
	}
	end = end.In(loc)
	day := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
	days := int(math.Ceil(*hours / DailyHours))
	if days == 0 {
		for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			day = day.AddDate(0, 0, -1)
		}
	} else {
		// Weekend deadlines use the next Monday as the subtraction anchor, so the
		// first counted workday is the preceding Friday, not an extra week earlier.
		for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			day = day.AddDate(0, 0, 1)
		}
		day = day.AddDate(0, 0, -7*(days/5))
		for left := days % 5; left > 0; {
			day = day.AddDate(0, 0, -1)
			if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				left--
			}
		}
	}
	if day.Year() < 1 || day.Year() > 9999 {
		return nil, memory.ErrInvalid
	}
	text := day.Format("2006-01-02")
	return &text, nil
}

// PlanTask sets a task's start date and its start reminder from its estimate
// and due time. The reminder fires at the owner's daily review time.
// Other kinds of item have no plan.
func PlanTask(item *workspace.Item, settings workspace.Settings, loc *time.Location) error {
	if item.Kind != "task" {
		return nil
	}
	var err error
	item.StartDate, err = StartDate(item.Due, item.EstimatedHours, loc)
	if err != nil {
		return err
	}
	kept := []workspace.Trigger{}
	for _, t := range item.Triggers {
		if t.ID != StartTrigger {
			kept = append(kept, t)
		}
	}
	item.Triggers = kept
	if item.StartDate != nil {
		date, err := time.ParseInLocation("2006-01-02", *item.StartDate, loc)
		if err != nil {
			return err
		}
		clock, err := time.Parse("15:04", settings.DailyReviewAt)
		if err != nil {
			return err
		}
		at := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
		item.Triggers = append(item.Triggers, workspace.Trigger{ID: StartTrigger, Kind: "time", NextAt: at.UTC().Format(time.RFC3339), Active: item.RemindersOn && (item.Status == "todo" || item.Status == "waiting"), Guard: item.Status, Description: "该开始了：按约" + FormatHours(*item.EstimatedHours) + "小时、每天4小时从截止倒推，开工日是" + *item.StartDate})
	}
	return nil
}

// FormatHours writes an estimate the way receipts and prompts show it.
func FormatHours(h float64) string {
	data, _ := json.Marshal(h)
	return strings.TrimSpace(string(data))
}
