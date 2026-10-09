package main

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type snapshotEvidence struct {
	SHA256 string         `json:"sha256"`
	Rows   int            `json:"rows"`
	Bytes  int64          `json:"bytes"`
	Tables map[string]int `json:"tables"`
}

type countingWriter struct {
	io.Writer
	bytes int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.bytes += int64(n)
	return n, err
}

// Stream every owner-scoped public table from one read-only snapshot. No fields
// are removed here: runtime-field and generated-ID comparison is a separate step.
func snapshot(ctx context.Context, pool *pgxpool.Pool, owner memory.ID, path string) (snapshotEvidence, error) {
	evidence := snapshotEvidence{Tables: map[string]int{}}
	path, err := privatePath(path)
	if err != nil {
		return evidence, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*")
	if err != nil {
		return evidence, err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return evidence, err
	}
	hash := sha256.New()
	compressed := gzip.NewWriter(f)
	stream := &countingWriter{Writer: io.MultiWriter(compressed, hash)}
	encoder := json.NewEncoder(stream)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		f.Close()
		return evidence, err
	}
	defer tx.Rollback(ctx)
	tables, err := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='owner_id' AND NOT a.attisdropped
 WHERE n.nspname='public' AND c.relkind IN ('r','p') AND NOT c.relispartition ORDER BY c.relname`)
	if err != nil {
		f.Close()
		return evidence, err
	}
	names := []string{}
	for tables.Next() {
		var name string
		if err = tables.Scan(&name); err != nil {
			break
		}
		names = append(names, name)
	}
	if err == nil {
		err = tables.Err()
	}
	tables.Close()
	if err != nil {
		f.Close()
		return evidence, err
	}
	for _, table := range names {
		// SQL ordering is unnecessary: comparison uses row multisets. In
		// particular, do not sort full vector and original-source documents.
		query := `SELECT to_jsonb(t) FROM ` + pgx.Identifier{"public", table}.Sanitize() + ` t WHERE owner_id=$1`
		rows, e := tx.Query(ctx, query, string(owner))
		if e != nil {
			err = e
			break
		}
		evidence.Tables[table] = 0
		for rows.Next() {
			var data json.RawMessage
			if err = rows.Scan(&data); err != nil {
				break
			}
			if err = encoder.Encode(struct {
				Table string          `json:"table"`
				Row   json.RawMessage `json:"row"`
			}{table, data}); err != nil {
				break
			}
			evidence.Tables[table]++
			evidence.Rows++
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			break
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	compressionErr := compressed.Close()
	if compressionErr == nil {
		compressionErr = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return evidence, err
	}
	if closeErr != nil {
		return evidence, closeErr
	}
	if compressionErr != nil {
		return evidence, compressionErr
	}
	if evidence.Rows == 0 {
		return evidence, errors.New("production_copy_snapshot_empty")
	}
	if err = os.Link(f.Name(), path); err != nil {
		return evidence, err
	}
	evidence.SHA256 = hex.EncodeToString(hash.Sum(nil))
	evidence.Bytes = stream.bytes
	return evidence, nil
}
