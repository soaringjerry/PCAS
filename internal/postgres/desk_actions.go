package postgres

import (
	"github.com/soaringjerry/PCAS/internal/workspace"
	"strings"
	"time"
)

// applyDueReminder maintains the one fixed reminder independently of other triggers.
// An empty remind preserves the existing offset; the secretary supplies defaults.
func applyDueReminder(item *workspace.Item, remind string, loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	existing := -1
	for i, t := range item.Triggers {
		if t.ID == "due-reminder" {
			existing = i
			if remind == "" {
				remind = t.Offset
			}
			break
		}
	}
	remove := func() {
		if existing >= 0 {
			item.Triggers = append(item.Triggers[:existing], item.Triggers[existing+1:]...)
		}
	}
	if item.Due == "" || remind == "none" {
		remove()
		return
	}
	if remind == "" {
		return
	}
	due, err := time.Parse(time.RFC3339, item.Due)
	if err != nil {
		return
	}
	next := due
	switch {
	case remind == "at":
	case strings.HasPrefix(remind, "-"):
		if !strings.HasSuffix(remind, "m") && !strings.HasSuffix(remind, "h") {
			return
		}
		offset, err := time.ParseDuration(remind)
		if err != nil || offset >= 0 {
			return
		}
		next = due.Add(offset)
	default:
		clock, err := time.Parse("15:04", remind)
		if err != nil {
			return
		}
		d := due.In(loc)
		next = time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	}
	trigger := workspace.Trigger{ID: "due-reminder", Kind: "time", Description: item.Title, NextAt: next.UTC().Format(time.RFC3339), Active: true, Offset: remind}
	if existing < 0 {
		item.Triggers = append(item.Triggers, trigger)
	} else {
		item.Triggers[existing] = trigger
	}
}
