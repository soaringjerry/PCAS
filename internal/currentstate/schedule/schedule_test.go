package schedule

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func entry(id, kind string) workspace.ScheduleEntry {
	return workspace.ScheduleEntry{ID: id, Kind: kind, Title: id}
}

func TestARecurringArrangementRepeatsAndAnUnclearOneWaits(t *testing.T) {
	q := workspace.ScheduleQuery{From: "2026-03-02", To: "2026-03-15"}
	weekly, _ := json.Marshal(map[string]any{"frequency": "weekly", "interval": 1, "anchor": "2026-03-02", "weekdays": []int{1}, "clock": "09:00"})
	out, err := Build(q, "Asia/Shanghai", []Deadline{
		{Entry: entry("w", "recurring"), Rule: weekly},
		{Entry: entry("legacy", "recurring"), Recurrence: "每天"},
		{Entry: entry("vague", "recurring"), Recurrence: "偶尔"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mondays := 0
	for _, day := range out.Days {
		for _, e := range day.Items {
			if e.ID == "w:"+day.Date && e.At != nil && *e.At == day.Date+"T09:00:00+08:00" {
				mondays++
			}
		}
	}
	if mondays != 2 || len(out.Days[0].Items) != 2 || out.Days[1].Items[0].TimeNote != "未说明具体时间" {
		t.Fatalf("%d %+v", mondays, out.Days[:2])
	}
	if len(out.Unclear) != 1 || out.Unclear[0].ID != "vague" || out.Unclear[0].TimeNote != "周期尚未明确，正在整理；" {
		t.Fatalf("%+v", out.Unclear)
	}
}

func TestADeadlineOrADueDateBeforeTheRangeIsOverdue(t *testing.T) {
	q := workspace.ScheduleQuery{From: "2026-03-02", To: "2026-03-03"}
	past := time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)
	tasks := []workspace.Item{
		{ID: "due", Title: "交表", Due: "2026-03-01T15:59:00Z"},
		{ID: "scheduled", Title: "开会", Scheduled: "2026-03-01"},
		{ID: "soon", Title: "体检", Due: "2026-03-02T15:59:00Z"},
	}
	out, err := Build(q, "Asia/Shanghai", []Deadline{{Entry: entry("d", "deadline"), At: &past}, {Entry: entry("a", "appointment"), At: &past}}, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Overdue) != 2 || out.Overdue[0].ID != "d" || out.Overdue[1].ID != "due:due" {
		t.Fatalf("%+v", out.Overdue)
	}
	// 23:59 without a stated time is a date.
	if e := out.Days[0].Items[0]; e.ID != "soon:due" || !e.DateOnly || e.At != nil || e.TimeNote != "只有日期" {
		t.Fatalf("%+v", e)
	}
}

func TestARangeIsAtMostItsLimit(t *testing.T) {
	if _, _, _, err := Window(workspace.ScheduleQuery{From: "2026-03-01", To: "2026-05-02"}, time.UTC); err == nil {
		t.Fatal("a range over 62 days was read")
	}
	if _, _, end, err := Window(workspace.ScheduleQuery{From: "2026-03-01", To: "2026-03-01"}, time.UTC); err != nil || !end.Equal(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(end, err)
	}
}

func TestTheHomePageListsTwelveUndatedTasksAndCountsTheRest(t *testing.T) {
	tasks := []workspace.Item{{ID: "dated", Due: "2026-03-01"}, {ID: "urgent", Urgent: true}}
	for i := 0; i < 14; i++ {
		tasks = append(tasks, workspace.Item{ID: "t"})
	}
	if got := InProgress(tasks); got.Total != 14 || len(got.Items) != InProgressLimit || got.Remaining != 2 {
		t.Fatalf("%+v", got)
	}
}
