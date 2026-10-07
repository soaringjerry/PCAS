package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
)

func TestPhase3P2T3BackwardDatesWeekendDSTAndNull(t *testing.T) {
	phase3Finding(t, "S-P3-005")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	// Frozen date oracles. Skeleton #262 documents dailyHours=4 and rounding UP
	// to whole business days. These expected dates are not calculated by product code.
	cases := []struct {
		zone, due string
		hours     any
		want      *string
	}{
		{"UTC", "2026-11-09T17:00:00Z", 8, phase3String("2026-11-05")},
		{"UTC", "2026-11-09T17:00:00Z", 4, phase3String("2026-11-06")},
		{"UTC", "2026-11-09T17:00:00Z", 4.01, phase3String("2026-11-05")},
		{"UTC", "2026-11-08T17:00:00Z", 0, phase3String("2026-11-06")},
		{"UTC", "2026-11-09T17:00:00Z", nil, nil},
		{"America/New_York", "2026-11-02T17:00:00-05:00", 8, phase3String("2026-10-29")},
		{"America/New_York", "2027-03-15T17:00:00-04:00", 8, phase3String("2027-03-11")},
		{"Asia/Shanghai", "2026-11-09T00:30:00+08:00", 8, phase3String("2026-11-05")},
	}
	for i, c := range cases {
		h.command(t, f.Context, map[string]any{"type": "updateSettings", "patch": map[string]string{"timezone": c.zone}})
		id := string(memory.NewID())
		h.command(t, f.Context, map[string]any{"type": "addTask", "id": id, "title": fmt.Sprintf("虚构日期边界%d", i), "projectId": p.ID, "due": c.due})
		h.command(t, f.Context, map[string]any{"type": "updateTask", "id": id, "patch": map[string]any{"estimatedHours": c.hours}})
		var timeline phase3Timeline
		h.get(t, f.Context, phase3ProjectPath(p.ID, "timeline"), &timeline)
		if timeline.DailyHours != 4 {
			t.Fatalf("documented default changed: %f", timeline.DailyHours)
		}
		found := false
		for _, it := range timeline.Items {
			if it.ID != id {
				continue
			}
			found = true
			if c.want == nil {
				if it.StartDate != nil {
					t.Error("nil effort has start date")
				}
			} else if it.StartDate == nil || *it.StartDate != *c.want {
				t.Errorf("zone=%s hours=%v due=%s start=%v want=%s", c.zone, c.hours, c.due, it.StartDate, *c.want)
			}
		}
		if !found {
			t.Fatal("missing date oracle task")
		}
	}
}
func phase3String(s string) *string { return &s }
func TestPhase3P1UserEstimateWinsOverInFlightModel(t *testing.T) {
	phase3Finding(t, "S-P3-005")
	f := phase3LoadFixture(t)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	m := phase3NewModel(t, f)
	stage := phase3StageName(t, "effort")
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	if err := phase3Schedule(f.Store, f.Context, "effort", time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	job := phase26ClaimStage(t, f.phase26LoadedFixture, stage)
	h := phase3NewHTTP(t, f)
	target := p.Items[1].ID
	entered, release := make(chan struct{}, 1), make(chan struct{})
	m.mu.Lock()
	m.Before = func(ctx context.Context, stage string) {
		if stage == "effort" {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- phase3Process(f.Store, f.Context, "effort", job) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("no in-flight estimate", err)
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("estimate barrier")
	}
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage == "secretary" {
			return `{"reply":"已把虚构工作量改为十二小时。","actions":[{"op":"update","ref":"THIS","set":{"estimatedHours":12}}],"used":[],"links":[],"show":[],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`
		}
		return out
	}
	m.mu.Unlock()
	code, turnRaw, err := h.call(f.Context, "POST", "/v1/desk/turn", map[string]any{"requestId": string(memory.NewID()), "agentId": "phase3", "thingId": target, "text": "这个要十二个小时。"})
	if err != nil || code != 200 {
		close(release)
		t.Fatal("user estimate via secretary", code, string(turnRaw), err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	h.get(t, f.Context, "/v1/workspace", &raw)
	var state struct {
		Tasks []struct {
			ID                         string
			EstimatedHours             *float64
			EffortSource, EffortReason string
		}
	}
	decodePhase3(t, raw, &state)
	for _, it := range state.Tasks {
		if it.ID == target {
			if it.EstimatedHours == nil || *it.EstimatedHours != 12 || it.EffortSource != "user" {
				t.Fatal("in-flight model overwrote authoritative user value", it)
			}
			return
		}
	}
	t.Fatal("missing user task")
}
func TestPhase3P3T3StartReminderTimingRecheckAndStop(t *testing.T) {
	for _, sequence := range []string{"boundary_once", "completed_before_dispatch", "already_doing", "user_stop"} {
		t.Run(sequence, func(t *testing.T) {
			phase3Finding(t, "S-P3-005")
			f := phase3LoadFixture(t)
			h := phase3NewHTTP(t, f)
			p := f.Gold.Projects[0]
			h.command(t, f.Context, map[string]any{"type": "updateSettings", "patch": map[string]any{"timezone": "UTC", "followUps": true}})
			id := string(memory.NewID())
			h.command(t, f.Context, map[string]any{"type": "addTask", "id": id, "title": "虚构需要该开始了的事项", "projectId": p.ID, "due": "2026-11-09T17:00:00Z"})
			h.command(t, f.Context, map[string]any{"type": "updateTask", "id": id, "patch": map[string]any{"estimatedHours": 8}})
			start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
			channel := &fakeNotifyChannel{name: "phase3-fictitious-channel"}
			channels := []notify.Channel{channel}
			run := func(at time.Time, dispatch bool) {
				t.Helper()
				if err := f.Store.CheckReminders(f.Context, at); err != nil {
					t.Fatal(err)
				}
				if dispatch {
					if err := f.Store.DispatchNotices(f.Context, at, channels); err != nil {
						t.Fatal(err)
					}
				}
			}
			count := func() int {
				n := 0
				for _, message := range channel.calls {
					if strings.Contains(message.URL, id) {
						n++
						if !strings.Contains(message.Body, "该开始") {
							t.Error("start reminder missing explanation", message)
						}
					}
				}
				return n
			}
			run(start.Add(-time.Nanosecond), true)
			if count() != 0 {
				t.Fatal("reminded before start date")
			}
			if sequence == "already_doing" {
				h.command(t, f.Context, map[string]any{"type": "setTaskStatus", "id": id, "status": "doing"})
			}
			run(start, false)
			if sequence == "completed_before_dispatch" {
				h.command(t, f.Context, map[string]any{"type": "setTaskStatus", "id": id, "status": "done"})
			}
			if sequence == "user_stop" {
				h.command(t, f.Context, map[string]any{"type": "updateTask", "id": id, "patch": map[string]any{"remindersOn": false}})
			}
			if err := f.Store.DispatchNotices(f.Context, start, channels); err != nil {
				t.Fatal(err)
			}
			run(start.Add(2*time.Hour), true)
			run(start.AddDate(0, 0, 1), true)
			want := 0
			if sequence == "boundary_once" {
				want = 1
			}
			if count() != want {
				t.Fatalf("sequence=%s sent=%d want=%d", sequence, count(), want)
			}
		})
	}
}
func TestPhase3P4T3TimelinePreservesAllStatusesAndUndated(t *testing.T) {
	phase3Finding(t, "S-P3-005")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	h.command(t, f.Context, map[string]any{"type": "updateSettings", "patch": map[string]string{"timezone": "UTC"}})
	pastID := string(memory.NewID())
	h.command(t, f.Context, map[string]any{"type": "addTask", "id": pastID, "title": "虚构截止已过的事项", "projectId": f.Gold.Projects[0].ID, "due": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)})
	for _, p := range f.Gold.Projects {
		var timeline phase3Timeline
		h.get(t, f.Context, phase3ProjectPath(p.ID, "timeline"), &timeline)
		dated, undated := map[string]bool{}, map[string]bool{}
		for _, it := range timeline.Items {
			dated[it.ID] = true
			if it.ID == pastID && !it.Overdue {
				t.Fatal("past deadline missing overdue marker", it)
			}
		}
		if p.ID == f.Gold.Projects[0].ID && !dated[pastID] {
			t.Fatal("past deadline omitted from timeline")
		}
		for _, it := range timeline.WithoutDue {
			undated[it.ID] = true
		}
		for _, gold := range p.Items {
			if gold.Due == "" {
				if !undated[gold.ID] || dated[gold.ID] {
					t.Error("undated task on axis/missing", gold.ID)
				}
			} else {
				if !dated[gold.ID] || undated[gold.ID] {
					t.Error("dated task filtered by status", gold.ID)
				}
				for _, it := range timeline.Items {
					if it.ID == gold.ID && it.Status != gold.Status {
						t.Error("timeline changed planted status", it)
					}
				}
			}
		}
	}
}
