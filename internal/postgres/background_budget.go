package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Contract 4.1: unused capacity is never borrowed by another stage.
var backgroundHourlyBudgets = map[string]int{
	OrganizeStage:         40,
	CompareStage:          40,
	EntityCompareStage:    30,
	EntityCandidatesStage: 6,
	HandoverStage:         2,
	ProjectHandoverStage:  10,
	EffortStage:           10,
	TopicProjectStage:     2,
	DateTidyStage:         dateTidyHourly,
}

func backgroundStage(stage string) string { return strings.SplitN(stage, ":", 2)[0] }

func backgroundHourlyTx(ctx context.Context, tx pgx.Tx, stage, code string) error {
	limit, ok := backgroundHourlyBudgets[stage]
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
