package postgres

import (
	"sort"
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryTimelineStatus(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		changed  bool
		want     string
	}{
		{"no items", nil, false, "open"},
		{"corrected without items", nil, true, "changed"},
		{"all done", []string{"done", "done"}, false, "done"},
		{"all dropped", []string{"cancelled", "dropped"}, false, "dropped"},
		{"unfinished", []string{"done", "todo"}, false, "open"},
		{"done and cancelled", []string{"done", "cancelled"}, false, "open"},
		{"waiting", []string{"waiting"}, false, "open"},
		{"items take precedence over correction", []string{"done"}, true, "done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := []secretaryTimelineTask{}
			for _, status := range tc.statuses {
				items = append(items, secretaryTimelineTask{Status: status})
			}
			if got := secretaryTimelineStatus(items, tc.changed); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSecretaryTimelineOrderAndVisibility(t *testing.T) {
	items := []workspace.DeskTimelineItem{
		{Text: "unknown first"},
		{Text: "later", At: stringPointer("2026-10-01T00:00:00Z")},
		{Text: "earlier", At: stringPointer("2026-10-01T08:00:00+10:00")},
		{Text: "same instant", At: stringPointer("2026-09-30T22:00:00Z")},
		{Text: "unknown second"},
	}
	sort.SliceStable(items, func(i, j int) bool { return secretaryTimelineBefore(items[i], items[j]) })
	for i, text := range []string{"earlier", "same instant", "later", "unknown first", "unknown second"} {
		if items[i].Text != text {
			t.Fatalf("wrong order at %d: %+v", i, items)
		}
	}
	for _, tc := range []struct {
		items  []workspace.DeskTimelineItem
		recall bool
		want   bool
	}{
		{nil, true, false},
		{items[:1], true, true},
		{items[:1], false, false},
		{items[3:4], true, false},
		{items, false, true},
	} {
		if got := secretaryTimelineVisible(tc.items, tc.recall); got != tc.want {
			t.Fatalf("visibility for %d items, recall=%v: got %v", len(tc.items), tc.recall, got)
		}
	}
}

func TestSecretaryTimelineTimeFallback(t *testing.T) {
	const said = "2025-11-03T12:00:00Z"
	const recorded = "2026-10-02T12:00:00Z"
	for _, tc := range []struct {
		name, expressed, sourceExpressed, connector, want string
	}{
		{"memory wins", said, recorded, "chatgpt", said},
		{"source expression wins", "", said, "chatgpt", said},
		{"desk capture", "", "", "desk", recorded},
		{"quick capture", "", "", "capture", recorded},
		{"telegram", "", "", "telegram", recorded},
		{"incomplete desk", "", "", "desk-incomplete", recorded},
		{"undated import", "", "", "chatgpt", ""},
		{"unknown origin", "", "", "manual", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := secretaryTimelineSource{
				SourceRef: workspace.SourceRef{At: recorded}, ExpressedAt: tc.sourceExpressed, Connector: tc.connector,
			}
			got := secretaryTimelineAt(tc.expressed, source)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("import date substituted: %s", *got)
				}
			} else if got == nil || *got != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}
