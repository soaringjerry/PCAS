package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/interpstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
)

func (s *Store) ScheduleStatus(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		return 0, memory.ErrInvalid
	}
	if _, ok := s.TextModel(); !ok {
		return 0, nil
	}
	count := 0
	rows, err := s.Pool.Query(ctx, "SELECT owner_id::text FROM workspace_owners ORDER BY owner_id")
	if err != nil {
		return 0, err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var owner memory.ID
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return count, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return count, err
	}
	for _, owner := range owners {
		err := kernel.BackgroundResultTx(ctx, s.Pool, owner, func(ctx context.Context, tx pgx.Tx) error {
			anchor, err := interpstore.SeedOrganizeGroupsTx(ctx, tx, owner)
			if err != nil {
				return err
			}
			topicCount, err := workspacestore.EnqueueTopicProjectsTx(ctx, tx, owner, now)
			if err != nil {
				return err
			}
			count += topicCount
			tidyCount, err := statestore.EnqueueDateTidyTx(ctx, tx, owner, now)
			if err != nil {
				return err
			}
			count += tidyCount
			n, err := statestore.EnqueueStatusHandoversTx(ctx, tx, map[memory.ID]memory.Ref{owner: anchor}, now)
			if err != nil {
				return err
			}
			count += n
			n, err = workspacestore.EnqueueProjectHandoversTx(ctx, tx, owner, anchor, now)
			count += n
			if err != nil {
				return err
			}
			n, err = enqueueEffortTx(ctx, tx, owner, anchor, now)
			count += n
			return err
		})
		if err != nil {
			if s.ScheduleYielded(ctx, owner, handover.Stage, err) {
				continue
			}
			return count, err
		}
	}
	return count, nil
}
