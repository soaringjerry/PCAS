package kernel

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/interpretation/aliasscan"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
	"github.com/soaringjerry/PCAS/internal/workspace/topicproject"
)

// DateTidyHourly is the number of date reviews that can reserve a call in
// one hour.
const DateTidyHourly = 30

// HourlyBudgets is the number of calls that each limited background stage can
// reserve in one hour. Contract 4.1: unused capacity is never borrowed by
// another stage. A stage that is absent has no stage limit.
var HourlyBudgets = map[string]int{
	organize.Stage:         40,
	comparison.Stage:       40,
	comparison.EntityStage: 30,
	aliasscan.Stage:        6,
	handover.Stage:         2,
	projecthandover.Stage:  10,
	effort.Stage:           10,
	topicproject.Stage:     2,
	datereview.Stage:       DateTidyHourly,
}

// BaseStage is the stage of a job without what follows its first colon.
func BaseStage(stage string) string { return strings.SplitN(stage, ":", 2)[0] }

// HourlyLimit returns the queue's deferral with code when the stage has used
// its calls of the last hour. The deferral ends when the oldest call leaves
// the hour.
func HourlyLimit(ctx context.Context, tx pgx.Tx, stage, code string) error {
	limit, ok := HourlyBudgets[stage]
	if !ok {
		return nil
	}
	var count int
	var next *time.Time
	if err := tx.QueryRow(ctx, `SELECT count(*),min(created_at)+interval '1 hour' FROM background_usage
 WHERE stage=$1 AND created_at>clock_timestamp()-interval '1 hour'`, stage).Scan(&count, &next); err != nil {
		return err
	}
	if count >= limit {
		return &worker.JobError{Code: code, Until: next.Add(time.Second), NoAttempt: true}
	}
	return nil
}
