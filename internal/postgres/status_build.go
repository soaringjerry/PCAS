package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type cardRule struct {
	N         int    `json:"n"`
	AppliesTo string `json:"appliesTo"`
}
type cardOutput struct {
	Fields    map[string][]int    `json:"fields"`
	Rules     []cardRule          `json:"rules"`
	Deadlines []organize.Deadline `json:"deadlines"`
}
type cardGroup struct {
	Key, Kind, Name string
	Entity          *string
	Count           int
	Built           *time.Time
	Rule            int
	Stale           bool
}

func (s *Store) RunStatus(ctx context.Context, logger *slog.Logger) {
	timer := time.NewTicker(kernel.StatusInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.RecordScheduleFailure(ctx, handover.Stage, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// Kept as a compatibility handler for pre-upgrade queue slots. Cards are frozen.
func (s *Store) ProcessCard(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}
