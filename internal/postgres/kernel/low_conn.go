package kernel

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
)

// Share the organizer's session lock across the model call. No owner lock is
// held while generating, so corrections and deletions can still proceed.
func (s *DB) ComparisonConnection(ctx context.Context) (*pgxpool.Conn, func(), error) {
	return s.OrganizeCallConnection(ctx, "compare_busy", CompareInterval)
}

// One organize, comparison, or entity scan call runs at a time. busy is the
// deferral code of the caller that finds the lock held.
func (s *DB) OrganizeCallConnection(ctx context.Context, busy string, wait time.Duration) (*pgxpool.Conn, func(), error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended(current_database()||':'||current_schema()||':organize-call',0))").Scan(&locked); err != nil || !locked {
		conn.Release()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, &worker.JobError{Code: busy, Until: time.Now().Add(wait), NoAttempt: true}
	}
	return conn, func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended(current_database()||':'||current_schema()||':organize-call',0))"); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}, nil
}

// Preserve the classifications unaffected by v3; v1 still owes the v2 deadline
// extraction. This comparison is used by scheduling, progress and consumers.
func OrganizeRequiredSQL(category, version string) string {
	return "(CASE WHEN " + version + "::int=3 AND " + category + "<>'rule' THEN 2 ELSE " + version + "::int END)"
}

func EmptyProjectHandover(id string) workspace.ProjectHandover { return projecthandover.Empty(id) }

func StatusCallLock(ctx context.Context, s *DB) (func(), error) {
	_, release, err := s.OrganizeCallConnection(ctx, "status_busy", StatusInterval)
	return release, err
}

func MaxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
