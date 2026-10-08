package postgres

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Wire adapter aligned to #276. No dependency on unmerged Go interface types.
type phase35Entry struct {
	ID, Title, Kind, Date, TimeNote, OriginalText string
	At                                            *string
	DateOnly                                      bool
	Source                                        struct{ Kind, MemoryID, ItemID, DeadlineID string }
}
type phase35Schedule struct {
	From, To, Timezone string
	Days               []struct {
		Date  string
		Items []phase35Entry
	}
	Unclear, Overdue []phase35Entry
}

func phase35ScheduleRead(t *testing.T, h *phase3HTTP, ctx context.Context, from, to string) phase35Schedule {
	t.Helper()
	var out phase35Schedule
	h.get(t, ctx, "/v1/workspace/schedule?from="+from+"&to="+to, &out)
	return out
}
func TestPhase35A1A2A3A4G6T4MonthScale(t *testing.T) {
	phase35Finding(t, "S-P35-001")
	f := phase35Load(t)
	h := phase35HTTP(t, f.Store, f.Scope)
	// Timed todo must mix between independent memory-based appointments.
	at := time.Date(2026, 10, 8, 15, 30, 0, 0, time.UTC).Format(time.RFC3339)
	st := h.command(t, f.Context, map[string]any{"type": "addTask", "title": "虚构时间线待办", "due": at})
	taskID := ""
	for _, it := range st.Tasks {
		if it.Title == "虚构时间线待办" {
			taskID = it.ID
		}
	}
	if taskID == "" {
		t.Fatal("precondition task missing")
	}
	before := phase35Digest(t, f)
	durations := []time.Duration{}
	var out phase35Schedule
	for i := 0; i < 8; i++ {
		start := time.Now()
		out = phase35ScheduleRead(t, h, f.Context, "2026-10-01", "2026-10-31")
		durations = append(durations, time.Since(start))
	}
	if out.Timezone != "UTC" || out.From != "2026-10-01" || out.To != "2026-10-31" || len(out.Days) != 31 {
		t.Fatalf("wrong range metadata %+v", out)
	}
	if len(out.Unclear) != 50 || len(out.Overdue) != 50 {
		t.Fatalf("unclear=%d overdue=%d want 50 each", len(out.Unclear), len(out.Overdue))
	}
	seen := map[string]int{}
	foundTask := false
	for i, d := range out.Days {
		want := fmt.Sprintf("2026-10-%02d", i+1)
		if d.Date != want {
			t.Fatalf("day=%s want=%s", d.Date, want)
		}
		times := []string{}
		for _, it := range d.Items {
			if it.Date != d.Date {
				t.Fatalf("instance out of civil date %+v", it)
			}
			if it.At != nil {
				x, err := time.Parse(time.RFC3339, *it.At)
				if err != nil {
					t.Fatal(err)
				}
				times = append(times, x.UTC().Format(time.RFC3339))
			}
			if it.Source.Kind == "deadline" {
				if it.Source.MemoryID == "" || it.Source.DeadlineID == "" || it.OriginalText == "" {
					t.Fatalf("missing evidence %+v", it)
				}
				seen[it.Source.DeadlineID]++
			}
			if it.Source.Kind == "task" && it.Source.ItemID == taskID {
				foundTask = true
				if it.Kind != "task" || it.At == nil || *it.At != at {
					t.Fatalf("wrong todo occurrence %+v", it)
				}
			}
		}
		if !sort.StringsAreSorted(times) {
			t.Fatalf("nonchronological day %s: %v", d.Date, times)
		}
	}
	for _, d := range f.Deadlines {
		want := 0
		switch {
		case d.Kind == "recurring":
			want = 31
		case d.At != nil && d.At.Month() == time.October:
			want = 1
		}
		if seen[d.ID] != want {
			t.Errorf("row %s kind=%s occurrences=%d want=%d", d.ID, d.Kind, seen[d.ID], want)
		}
	}
	if !foundTask {
		t.Error("timed todo absent from mixed schedule")
	}
	for _, it := range out.Unclear {
		if it.At != nil || it.OriginalText == "" {
			t.Fatalf("unclear fabricated time or lost speech %+v", it)
		}
	}
	after := phase35Digest(t, f)
	if !reflect.DeepEqual(before, after) {
		for table, v := range before {
			if after[table] != v {
				t.Errorf("G6 read wrote %s", table)
			}
		}
	}
	phase3Latency(t, "phase35_month_5000_memories_200_deadlines", durations)
	for _, path := range []string{"?from=2026-10-01&to=2026-12-02", "?from=2026-10-31&to=2026-10-01", "?from=invalid&to=2026-10-31"} {
		code, raw, err := h.call(f.Context, "GET", "/v1/workspace/schedule"+path, nil)
		if err != nil || code != 400 {
			t.Fatalf("range bound %s status=%d body=%s err=%v", path, code, raw, err)
		}
	}
}
func TestPhase35T3DSTAndTimezoneCivilOccurrences(t *testing.T) {
	phase35Finding(t, "S-P35-001")
	f := phase35Load(t)
	h := phase35HTTP(t, f.Store, f.Scope)
	// One recurring row isolated; persisted instant remains merely the input anchor.
	phase26Exec(t, f.phase26LoadedFixture, `DELETE FROM deadlines WHERE owner_id=$1 AND id<>$2`, f.Scope.OwnerID, f.Deadlines[2].ID)
	for _, zone := range []string{"America/New_York", "Asia/Shanghai", "UTC"} {
		h.command(t, f.Context, map[string]any{"type": "updateSettings", "patch": map[string]any{"timezone": zone}})
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		for _, bounds := range [][2]string{{"2026-03-07", "2026-03-09"}, {"2026-10-31", "2026-11-02"}} {
			before := phase35Digest(t, f)
			out := phase35ScheduleRead(t, h, f.Context, bounds[0], bounds[1])
			if out.Timezone != zone || len(out.Days) != 3 {
				t.Fatalf("timezone/range %+v", out)
			}
			for _, day := range out.Days {
				if len(day.Items) != 1 {
					t.Fatalf("%s %s occurrence count=%d", zone, day.Date, len(day.Items))
				}
				it := day.Items[0]
				if it.At == nil {
					t.Fatal("timed recurrence lost time")
				}
				at, err := time.Parse(time.RFC3339, *it.At)
				if err != nil {
					t.Fatal(err)
				}
				if at.In(loc).Format("2006-01-02 15:04") != day.Date+" 09:00" {
					t.Fatalf("DST drift zone=%s got=%s day=%s", zone, *it.At, day.Date)
				}
			}
			if !reflect.DeepEqual(before, phase35Digest(t, f)) {
				t.Fatal("DST read materialized occurrences")
			}
		}
	}
}
func TestPhase35T3InclusiveThreeDayAndMidnightBoundary(t *testing.T) {
	phase35Finding(t, "S-P35-001")
	f := phase35Load(t)
	h := phase35HTTP(t, f.Store, f.Scope)
	h.command(t, f.Context, map[string]any{"type": "updateSettings", "patch": map[string]any{"timezone": "Asia/Shanghai"}})
	// Local 00:00 on day+3 included; day+4 exactly 00:00 excluded.
	phase26Exec(t, f.phase26LoadedFixture, `DELETE FROM deadlines WHERE owner_id=$1`, f.Scope.OwnerID)
	for i, at := range []string{"2026-10-10T15:59:59Z", "2026-10-10T16:00:00Z", "2026-10-11T15:59:59Z", "2026-10-11T16:00:00Z"} {
		phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,title,original_text) VALUES($1,$2,$3,1,'deadline',$4,$5,$5)`, f.Scope.OwnerID, memory.NewID(), f.Claims[2200+i], at, fmt.Sprintf("虚构边界%d", i))
	}
	out := phase35ScheduleRead(t, h, f.Context, "2026-10-08", "2026-10-11")
	got := map[string]string{}
	for _, day := range out.Days {
		for _, it := range day.Items {
			got[it.Title] = day.Date
		}
	}
	want := map[string]string{"虚构边界0": "2026-10-10", "虚构边界1": "2026-10-11", "虚构边界2": "2026-10-11"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("three-day inclusive boundary=%v want=%v", got, want)
	}
}
func TestPhase35D1D2G1G6InProgressCapAndOrder(t *testing.T) {
	phase35Finding(t, "S-P35-002")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	h := phase35HTTP(t, s, scope)
	phase35Snapshot(t, s, scope)
	for i := 0; i < 17; i++ {
		title := fmt.Sprintf("虚构推进%02d", i)
		h.command(t, ctx, map[string]any{"type": "addTask", "title": title})
	}
	h.command(t, ctx, map[string]any{"type": "addTask", "title": "虚构有日期", "due": "2026-11-03T00:00:00Z"})
	stUrgent := h.command(t, ctx, map[string]any{"type": "addTask", "title": "虚构尽快"})
	for _, it := range stUrgent.Tasks {
		if it.Title == "虚构尽快" {
			h.command(t, ctx, map[string]any{"type": "updateTask", "id": it.ID, "patch": map[string]any{"urgent": true}})
		}
	}
	stScheduled := h.command(t, ctx, map[string]any{"type": "addTask", "title": "虚构有安排"})
	for _, it := range stScheduled.Tasks {
		if it.Title == "虚构有安排" {
			h.command(t, ctx, map[string]any{"type": "updateTask", "id": it.ID, "patch": map[string]any{"scheduled": "2026-11-02T10:00:00Z"}})
		}
	}
	// Stable progress timestamps; expected titles independent of returned order.
	_, err := s.pool.Exec(ctx, `UPDATE work_items SET document=document||jsonb_build_object('updatedAt',to_char('2026-10-08T12:00:00Z'::timestamptz-(substring(title from '[0-9]+$')::int)*interval '1 hour','YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE owner_id=$1 AND title LIKE '虚构推进%'`, scope.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	f := &phase35Fixture{phase26LoadedFixture: &phase26LoadedFixture{Store: s, Context: ctx, Scope: scope}}
	before := phase35Digest(t, f)
	var out struct {
		Items            []workspace.Item
		Total, Remaining int
	}
	h.get(t, ctx, "/v1/workspace/in-progress", &out)
	if len(out.Items) != 12 || out.Total != 17 || out.Remaining != 5 {
		t.Fatalf("cap/destination len=%d total=%d remaining=%d", len(out.Items), out.Total, out.Remaining)
	}
	for i, it := range out.Items {
		if it.Title != fmt.Sprintf("虚构推进%02d", i) {
			t.Fatalf("latest progress order i=%d got=%q", i, it.Title)
		}
	}
	if !reflect.DeepEqual(before, phase35Digest(t, f)) {
		t.Fatal("in-progress GET wrote")
	}
	if len(phase35Snapshot(t, s, scope).Tasks) != 20 {
		t.Fatal("display cap discarded work")
	}
}
