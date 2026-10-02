package postgres

import (
	"github.com/soaringjerry/PCAS/internal/workspace"
	"testing"
	"time"
)

func TestMemoryPromptSuffix(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		m    workspace.Memory
		want string
	}{
		{name: "old"},
		{name: "local speech", m: workspace.Memory{ExpressedAt: "2025-11-02T14:00:00Z"}, want: " / 说于 2025-11-03"},
		{name: "month", m: workspace.Memory{EventFrom: "2025-11-01T00:00:00+11:00", EventPrecision: "month"}, want: " / 事件 2025-11"},
		{name: "year", m: workspace.Memory{EventFrom: "2025-01-01T00:00:00+11:00", EventPrecision: "year"}, want: " / 事件 2025"},
		{name: "day", m: workspace.Memory{EventFrom: "2025-06-12T00:00:00+10:00", EventPrecision: "day"}, want: " / 事件 2025-06-12"},
		{name: "exclusive range", m: workspace.Memory{EventFrom: "2025-06-12T00:00:00+10:00", EventTo: "2025-06-15T00:00:00+10:00", EventPrecision: "range"}, want: " / 事件 2025-06-12 至 2025-06-14"},
		{name: "DST range", m: workspace.Memory{EventFrom: "2025-10-04T00:00:00+10:00", EventTo: "2025-10-06T00:00:00+11:00", EventPrecision: "range"}, want: " / 事件 2025-10-04 至 2025-10-05"},
		{name: "unknown", m: workspace.Memory{EventFrom: "2025-01-01T00:00:00Z", EventPrecision: "unknown"}},
		{name: "invalid", m: workspace.Memory{ExpressedAt: "no date", EventFrom: "invalid"}},
		{name: "mentions", m: workspace.Memory{Mentions: []workspace.MemoryMention{{Name: "成都", Role: "place"}, {Name: "老王", Role: "person"}, {Name: "公司", Role: "organization"}}}, want: " / 成都 / 老王"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := memoryPromptSuffix(c.m, loc); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
