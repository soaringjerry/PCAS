package app

import (
	"os"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/postgres"
)

func TestResponseTableNamesEachStageOnceAndCoversEveryLimitedStage(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Responses {
		if seen[r.Stage] || r.Stage == "" || r.Trigger == "" || r.Producer == "" || r.Specialist == "" || r.Duplicate == "" || r.handler == nil {
			t.Errorf("incomplete or repeated row: %+v", r.Stage)
		}
		seen[r.Stage] = true
	}
	for stage := range postgres.StageHourlyLimits() {
		if !seen[stage] {
			t.Errorf("stage %s has an hourly limit and no response row", stage)
		}
	}
}

// The architecture page shows the same table that the worker registers.
// When this fails, copy the output of ResponseTable into the page.
func TestArchitecturePageShowsTheRegisteredResponseTable(t *testing.T) {
	page, err := os.ReadFile("../../docs/architecture.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), ResponseTable()) {
		t.Fatal("docs/architecture.md does not contain the registered table:\n" + ResponseTable())
	}
}
