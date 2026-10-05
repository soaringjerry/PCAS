package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) listRetiredMemories(ctx context.Context, scope memory.Scope, q workspace.MemoryQuery) (workspace.MemoryPage, error) {
	out := workspace.MemoryPage{Items: []workspace.Memory{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if err := validateMemoryQuery(&q); err != nil {
		return out, err
	}
	opts := memoryReadOptions{query: q, limit: q.Limit + 1}
	if q.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(data) > 1024 {
			return out, memory.ErrInvalid
		}
		var cursor memoryCursor
		if strictJSON(data, &cursor) != nil || !memory.ID(cursor.ID).Valid() || cursor.At.IsZero() || cursor.Snapshot.IsZero() {
			return out, memory.ErrInvalid
		}
		opts.before = &cursor
		opts.snapshot = cursor.Snapshot
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if q.Trust != "" {
			if _, err := tx.Exec(ctx, "SET LOCAL jit=off"); err != nil {
				return err
			}
		}
		if opts.snapshot.IsZero() {
			if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&opts.snapshot); err != nil {
				return err
			}
		}
		if q.Trust != "" {
			var err error
			out.Total, opts.ids, err = trustedMemoryIDsTx(ctx, tx, scope, opts)
			if err != nil {
				return err
			}
			opts.query.Trust = ""
		} else {
			countOpts := opts
			countOpts.before = nil
			where, args := memoryWhere(scope, false, countOpts)
			if err := tx.QueryRow(ctx, "SELECT count(*)"+memoryJoins+where, args...).Scan(&out.Total); err != nil {
				return err
			}
		}
		items, err := s.readMemoriesTx(ctx, tx, scope, false, opts)
		if err != nil {
			return err
		}
		if len(items) > q.Limit {
			items = items[:q.Limit]
			last := items[len(items)-1]
			var at time.Time
			if err := tx.QueryRow(ctx, "SELECT updated_at FROM memory_records WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), last.ID).Scan(&at); err != nil {
				return err
			}
			data, err := json.Marshal(memoryCursor{At: at, ID: last.ID, Snapshot: opts.snapshot})
			if err != nil {
				return err
			}
			out.Next = base64.RawURLEncoding.EncodeToString(data)
		}
		out.Items = items
		return nil
	})
	return out, err
}
