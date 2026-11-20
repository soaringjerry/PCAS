package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

type syncLease = connectors.Lease

func (s *Store) RunConnectors(ctx context.Context, logger *slog.Logger) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.syncNextConnection(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("connector sync failed; details available in connection status")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Store) syncNextConnection(ctx context.Context) error {
	lease := syncLease{Token: memory.NewID()}
	err := s.Pool.QueryRow(ctx, `WITH candidate AS (SELECT id FROM connector_configs WHERE enabled AND kind IN ('poll','folder') AND next_sync<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_sync FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE connector_configs c SET lease_token=$1,lease_until=now()+interval '2 minutes',status='syncing' FROM candidate n WHERE c.id=n.id RETURNING c.owner_id::text,c.id::text,c.name,c.kind,c.url,c.token_env,c.version,c.cursor,c.etag`, string(lease.Token)).Scan(&lease.Owner, &lease.Connection.ID, &lease.Connection.Name, &lease.Connection.Kind, &lease.Connection.URL, &lease.Connection.TokenEnv, &lease.Connection.Version, &lease.Cursor, &lease.ETag)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err = connectors.Sync(work, syncStore{s}, lease)
	if err != nil {
		_, _ = s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE connector_configs SET status='error',error_code=$3,next_sync=now()+interval_seconds*interval '1 second',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, string(lease.Connection.ID), string(lease.Token), connectors.SyncError(err))
	}
	return err
}
func (s *Store) finishSync(ctx context.Context, l syncLease, b connectors.Batch, etag string, files map[string]string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var exists bool
		err := tx.QueryRow(ctx, "SELECT true FROM connector_configs WHERE id=$1 AND lease_token=$2 AND enabled AND lease_until>clock_timestamp() FOR UPDATE", string(l.Connection.ID), string(l.Token)).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrConflict
		}
		if err != nil {
			return err
		}
		imported := 0
		gaps := b.Gaps
		if gaps == nil {
			gaps = []string{}
		}
		if len(b.Records) > 0 {
			result, err := s.ImportBatchTx(ctx, tx, memory.Scope{OwnerID: l.Owner, PrincipalID: "connector", IsOwner: true}, "connection:"+string(l.Connection.ID), b, nil)
			if err != nil {
				return err
			}
			imported = result.Imported
			gaps = result.Gaps
		}
		for path, fingerprint := range files {
			hash := sha256.Sum256([]byte(path))
			if _, err := tx.Exec(ctx, `INSERT INTO connector_files(owner_id,connector_id,path_hash,fingerprint) VALUES($1,$2,$3,$4) ON CONFLICT(owner_id,connector_id,path_hash) DO UPDATE SET fingerprint=excluded.fingerprint`, string(l.Owner), string(l.Connection.ID), hash[:], fingerprint); err != nil {
				return err
			}
		}
		cursor := b.NextCursor
		if cursor == "" {
			cursor = l.Cursor
		}
		if b.HasMore {
			etag = ""
		}
		_, err = tx.Exec(ctx, `UPDATE connector_configs SET cursor=$3,etag=$4,status='idle',error_code='',gaps=$5,imported=imported+$6,last_sync=now(),next_sync=now()+CASE WHEN $7 THEN 0 ELSE interval_seconds END*interval '1 second',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, string(l.Connection.ID), string(l.Token), cursor, etag, kernel.JSON(gaps), imported, b.HasMore)
		return err
	})
}

// syncStore is the storage of one connector sync.
type syncStore struct{ store *Store }

func (u syncStore) Finish(ctx context.Context, l connectors.Lease, b connectors.Batch, etag string, files map[string]string) error {
	return u.store.finishSync(ctx, l, b, etag, files)
}

func (u syncStore) Fingerprint(ctx context.Context, l connectors.Lease, pathHash []byte) (string, error) {
	var previous string
	err := u.store.Pool.QueryRow(ctx, "SELECT fingerprint FROM connector_files WHERE owner_id=$1 AND connector_id=$2 AND path_hash=$3", string(l.Owner), string(l.Connection.ID), pathHash).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return previous, err
}

func (u syncStore) IngestArchive(ctx context.Context, scope memory.Scope, name string, data []byte, origin string) error {
	_, err := u.store.IngestArchive(ctx, scope, name, data, origin)
	return err
}
