package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type syncLease struct {
	Owner        memory.ID
	Connection   connectors.Connection
	Token        memory.ID
	Cursor, ETag string
}

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
	err := s.pool.QueryRow(ctx, `WITH candidate AS (SELECT id FROM connector_configs WHERE enabled AND kind IN ('poll','folder') AND next_sync<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_sync FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE connector_configs c SET lease_token=$1,lease_until=now()+interval '2 minutes',status='syncing' FROM candidate n WHERE c.id=n.id RETURNING c.owner_id::text,c.id::text,c.name,c.kind,c.url,c.token_env,c.version,c.cursor,c.etag`, string(lease.Token)).Scan(&lease.Owner, &lease.Connection.ID, &lease.Connection.Name, &lease.Connection.Kind, &lease.Connection.URL, &lease.Connection.TokenEnv, &lease.Connection.Version, &lease.Cursor, &lease.ETag)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if lease.Connection.Kind == "folder" {
		err = s.syncFolder(work, lease)
	} else {
		err = s.syncHTTP(work, lease)
	}
	if err != nil {
		_, _ = s.pool.Exec(context.WithoutCancel(ctx), `UPDATE connector_configs SET status='error',error_code=$3,next_sync=now()+interval_seconds*interval '1 second',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, string(lease.Connection.ID), string(lease.Token), syncError(err))
	}
	return err
}
func syncError(err error) string {
	if errors.Is(err, memory.ErrConflict) {
		return "source_version_conflict"
	}
	if errors.Is(err, memory.ErrInvalid) {
		return "invalid_source_format"
	}
	if errors.Is(err, memory.ErrForbidden) {
		return "access_denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "sync_timeout"
	}
	return "source_unavailable"
}
func (s *Store) syncHTTP(ctx context.Context, l syncLease) error {
	u, err := url.Parse(l.Connection.URL)
	if err != nil {
		return err
	}
	q := u.Query()
	if l.Cursor != "" {
		q.Set("cursor", l.Cursor)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if l.ETag != "" {
		req.Header.Set("If-None-Match", l.ETag)
	}
	if l.Connection.TokenEnv != "" {
		token := os.Getenv(l.Connection.TokenEnv)
		if token == "" {
			return memory.ErrForbidden
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return s.finishSync(ctx, l, connectors.Batch{}, l.ETag, nil)
	}
	if response.StatusCode != 200 {
		return memory.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		return memory.ErrInvalid
	}
	var b connectors.Batch
	if json.Unmarshal(data, &b) != nil || len(b.Records) > 100 || len(b.NextCursor) > 4096 || len(response.Header.Get("ETag")) > 4096 {
		return memory.ErrInvalid
	}
	if len(b.Records) > 0 {
		b, err = connectors.Normalize(b)
		if err != nil {
			return memory.ErrInvalid
		}
	}
	if b.HasMore && (b.NextCursor == "" || b.NextCursor == l.Cursor) {
		return memory.ErrInvalid
	}
	return s.finishSync(ctx, l, b, response.Header.Get("ETag"), nil)
}
func (s *Store) syncFolder(ctx context.Context, l syncLease) error {
	root := os.Getenv("PCAS_INBOX_DIR")
	if root == "" {
		return memory.ErrUnavailable
	}
	// os.Root rejects symlink escapes and traversal, including concurrent changes.
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err = directory.Mkdir(string(l.Connection.ID), 0700); err != nil && !os.IsExist(err) {
		return err
	}
	folder, err := directory.OpenRoot(string(l.Connection.ID))
	if err != nil {
		return err
	}
	defer folder.Close()
	names, err := folder.Open(".")
	if err != nil {
		return err
	}
	entries, err := names.ReadDir(1001)
	names.Close()
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > 1000 {
		return memory.ErrInvalid
	}
	batch := connectors.Batch{Records: []connectors.Record{}, Gaps: []string{}}
	fingerprints := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !oneOf(ext, ".txt", ".md", ".json", ".jsonl", ".zip") {
			continue
		}
		file, err := folder.Open(entry.Name())
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
		file.Close()
		if err != nil || len(data) > 8<<20 {
			return memory.ErrInvalid
		}
		sum := sha256.Sum256(data)
		fingerprint := hex.EncodeToString(sum[:])
		pathHash := sha256.Sum256([]byte(entry.Name()))
		var previous string
		err = s.pool.QueryRow(ctx, "SELECT fingerprint FROM connector_files WHERE owner_id=$1 AND connector_id=$2 AND path_hash=$3", string(l.Owner), string(l.Connection.ID), pathHash[:]).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if previous == fingerprint {
			continue
		}
		decoded, err := connectors.DecodeArchive(entry.Name(), data)
		if err != nil {
			return memory.ErrInvalid
		}
		if len(batch.Records)+len(decoded.Records) > 10000 {
			return memory.ErrInvalid
		}
		// The entire original export is retained before its normalized messages.
		scope := memory.Scope{OwnerID: l.Owner, PrincipalID: "connector", IsOwner: true}
		if ext == ".zip" || ext == ".json" || ext == ".jsonl" {
			if _, err = s.ingestArchive(ctx, scope, entry.Name(), data, "connection-archive:"+string(l.Connection.ID)); err != nil && !errors.Is(err, memory.ErrBlocked) {
				return err
			}
		} else {
			batch.Records = append(batch.Records, decoded.Records...)
		}
		batch.Gaps = append(batch.Gaps, decoded.Gaps...)
		fingerprints[entry.Name()] = fingerprint
	}
	return s.finishSync(ctx, l, batch, "", fingerprints)
}
func (s *Store) finishSync(ctx context.Context, l syncLease, b connectors.Batch, etag string, files map[string]string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
			result, err := s.importBatchTx(ctx, tx, memory.Scope{OwnerID: l.Owner, PrincipalID: "connector", IsOwner: true}, "connection:"+string(l.Connection.ID), b, nil)
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
		_, err = tx.Exec(ctx, `UPDATE connector_configs SET cursor=$3,etag=$4,status='idle',error_code='',gaps=$5,imported=imported+$6,last_sync=now(),next_sync=now()+CASE WHEN $7 THEN 0 ELSE interval_seconds END*interval '1 second',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, string(l.Connection.ID), string(l.Token), cursor, etag, asJSON(gaps), imported, b.HasMore)
		return err
	})
}
