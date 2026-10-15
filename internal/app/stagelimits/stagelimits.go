// Package stagelimits collects the hourly call limit of each limited
// background stage from the domain that owns the stage. The application gives
// the table to storage when it opens the store.
package stagelimits

import (
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/interpretation/aliasscan"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
	"github.com/soaringjerry/PCAS/internal/workspace/topicproject"
)

// Hourly returns the number of calls that each limited stage can reserve in
// one hour. A stage that is absent has no stage limit. Contract 4.1: unused
// capacity is never borrowed by another stage.
func Hourly() map[string]int {
	return map[string]int{
		organize.Stage:         organize.HourlyCalls,
		comparison.Stage:       comparison.HourlyCalls,
		comparison.EntityStage: comparison.EntityHourlyCalls,
		aliasscan.Stage:        aliasscan.HourlyCalls,
		handover.Stage:         handover.HourlyCalls,
		projecthandover.Stage:  projecthandover.HourlyCalls,
		effort.Stage:           effort.HourlyCalls,
		topicproject.Stage:     topicproject.HourlyCalls,
		datereview.Stage:       datereview.HourlyCalls,
	}
}
