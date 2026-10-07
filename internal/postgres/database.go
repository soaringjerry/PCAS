// Package postgres implements canonical persistence and the transactional queue.
package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	pool        *pgxpool.Pool
	pendingPaid sync.Map
	// DeskTurn also borrows a connection for Recall and budget reservation.
	secretarySlots chan struct{}
	models         *ai.Registry
	blobs          memory.BlobStore
}

func (s *Store) SetModels(models *ai.Registry)   { s.models = models }
func (s *Store) SetBlobs(blobs memory.BlobStore) { s.blobs = blobs }

func Open(ctx context.Context, url string) (*Store, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	config.MaxConns = 10
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database unavailable")
	}
	return &Store{pool: pool, secretarySlots: make(chan struct{}, config.MaxConns-1)}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Migrate serializes concurrent runners and checks applied migration checksums.
// Each file and its bookkeeping commit atomically; no destructive down command.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(734826190)"); err != nil {
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(734826190)"); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	_, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, file := range files {
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		var applied string
		err = conn.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", file.Name()).Scan(&applied)
		if err == nil {
			if applied != checksum {
				return fmt.Errorf("migration %s was modified after application", file.Name())
			}
			continue
		}
		if err != pgx.ErrNoRows {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), checksum)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", file.Name(), err)
		}
	}
	return nil
}

func (s *Store) CheckSchema(ctx context.Context) error {
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, file := range files {
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		var checksum string
		if err := s.pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", file.Name()).Scan(&checksum); err != nil {
			return fmt.Errorf("database schema is not ready; run pcas migrate")
		}
		if checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
			return fmt.Errorf("migration checksum mismatch: %s", file.Name())
		}
	}
	return nil
}
