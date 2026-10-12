package postgres

import (
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestOrderTeamExcerpts(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	inside := from.AddDate(0, 1, 0)
	original := []memory.RecallExcerpt{{Text: "import", Connector: "file", RecordedAt: inside}, {Text: "direct", Connector: "capture", RecordedAt: inside}, {Text: "end", ExpressedAt: &to}, {Text: "expressed", ExpressedAt: &from}, {Text: "old", RecordedAt: from.AddDate(-1, 0, 0)}}
	noPlan := append([]memory.RecallExcerpt{}, original...)
	contextbuilder.OrderExcerpts(noPlan, memory.QueryPlan{})
	for i := range original {
		if original[i].Text != noPlan[i].Text {
			t.Fatal("no condition reordered")
		}
	}
	contextbuilder.OrderExcerpts(original, memory.QueryPlan{Time: &memory.PlanTime{From: from, To: to}})
	for i, want := range []string{"direct", "expressed", "import", "end", "old"} {
		if original[i].Text != want {
			t.Fatalf("index %d got %s want %s", i, original[i].Text, want)
		}
	}
}
