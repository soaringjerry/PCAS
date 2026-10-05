package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

const backgroundWriteTimeout = 2 * time.Second

// Derived writes fence user mutations with KEY SHARE, which is compatible
// with snapshot's SHARE lock. Never keep this transaction over a model call.
// Busy rows yield promptly rather than holding the owner while waiting.
func backgroundWriteTx(ctx context.Context, db interface {
	Begin(context.Context) (pgx.Tx, error)
}, owner memory.ID, write func(context.Context, pgx.Tx) error) error {
	writeCtx, cancel := context.WithTimeout(ctx, backgroundWriteTimeout)
	defer cancel()
	err := pgx.BeginFunc(writeCtx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(writeCtx, "SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='1500ms'"); err != nil {
			return err
		}
		if _, err := tx.Exec(writeCtx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR KEY SHARE", string(owner)); err != nil {
			return err
		}
		return write(writeCtx, tx)
	})
	return backgroundWriteError(ctx, err)
}

func backgroundWriteError(ctx context.Context, err error) error {
	var pgErr *pgconn.PgError
	if ctx.Err() == nil && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "57014")) {
		return &worker.JobError{Code: "background_write_busy", Until: time.Now().Add(time.Second), NoAttempt: true}
	}
	return err
}

// A model result is already paid for: wait out a brief user write before
// yielding the job. write must be safe to run again.
func backgroundResultTx(ctx context.Context, db interface {
	Begin(context.Context) (pgx.Tx, error)
}, owner memory.ID, write func(context.Context, pgx.Tx) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = backgroundWriteTx(ctx, db, owner, write)
		var busy *worker.JobError
		if !errors.As(err, &busy) || busy.Code != "background_write_busy" {
			return err
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return err
		}
	}
	return err
}
