package kernel

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// BaseStage is the stage of a job without what follows its first colon.
func BaseStage(stage string) string { return strings.SplitN(stage, ":", 2)[0] }

// Limited reports whether the stage has an hourly call limit. A limited stage
// keeps its paid results and retries until it succeeds or its input changes.
func (d *DB) Limited(stage string) bool {
	_, ok := d.hourly[BaseStage(stage)]
	return ok
}

// HourlyLimits returns a copy of the number of calls that each limited stage
// can reserve in one hour.
func (d *DB) HourlyLimits() map[string]int {
	out := make(map[string]int, len(d.hourly))
	for stage, limit := range d.hourly {
		out[stage] = limit
	}
	return out
}

// HourlyLimit returns the queue's deferral with code when the stage has used
// its calls of the last hour. The deferral ends when the oldest call leaves
// the hour.
func (d *DB) HourlyLimit(ctx context.Context, tx pgx.Tx, stage, code string) error {
	limit, ok := d.hourly[stage]
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
