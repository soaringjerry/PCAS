package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	CompareVersion  = 1
	CardVersion     = 1
	HandoverVersion = 1
)

// S0 only defines the shared read contract. Batch 3 supplies the data.
func (s *Store) HandoverTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.Handover, error) {
	return workspace.Handover{}, nil
}

func (s *Store) StatusCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, keys []string) ([]workspace.StatusCard, error) {
	return []workspace.StatusCard{}, nil
}

func (s *Store) DeadlinesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, now time.Time, limit int) ([]workspace.Deadline, error) {
	return []workspace.Deadline{}, nil
}

func (s *Store) StatusCardIndexTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) ([]workspace.StatusCardRef, error) {
	return []workspace.StatusCardRef{}, nil
}

func (s *Store) About(ctx context.Context, scope memory.Scope, key string) (workspace.About, error) {
	out := workspace.About{Cards: []workspace.StatusCard{}, Deadlines: []workspace.Deadline{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out.Handover, err = s.HandoverTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if key != "" {
			out.Cards, err = s.StatusCardsTx(ctx, tx, scope, []string{key})
		} else {
			var index []workspace.StatusCardRef
			index, err = s.StatusCardIndexTx(ctx, tx, scope)
			for _, ref := range index {
				out.Cards = append(out.Cards, workspace.StatusCard{
					Key: ref.Key, Kind: ref.Kind, Name: ref.Name, Count: ref.Count,
					BuiltAt: ref.BuiltAt, Stale: ref.Stale, Fields: []workspace.StatusCardField{},
				})
			}
		}
		if err != nil {
			return err
		}
		out.Deadlines, err = s.DeadlinesTx(ctx, tx, scope, time.Now(), 15)
		return err
	})
	return out, err
}
