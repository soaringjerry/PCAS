// Package postgres implements canonical persistence and the transactional queue.
package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/activitystore"
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/evidencestore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/interpstore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/memorystore"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/schedulestore"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
	"github.com/soaringjerry/PCAS/internal/postgres/usagelog"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the PostgreSQL adapter. Its core is the kernel's DB, which the
// storage packages share.
type Store struct {
	*kernel.DB
	*interpstore.Interp
	*workspacestore.Workspace
	*intakestore.Intake
	*callstore.Calls
	*statestore.State
	*evidencestore.Evidence
	*workstore.Work
	*budgetstore.Budgets
	*claimstore.Claims
	*sourcestore.Sources
	*itemstore.Items
	*ownerstore.Owners
	*activitystore.Activity
	*schedulestore.Schedules
	*usagelog.Usage
	*memorystore.Memory
	*memoryread.Reader
	callstore.CallPending
}

// SetModels gives the store the configured models. The store reads their
// configuration and availability. It makes no model call: the application
// builds the gateway over CallRecords.
func (s *Store) SetModels(models *ai.Registry) { s.Models = models }

func (s *Store) SetBlobs(blobs memory.BlobStore) { s.Blobs = blobs }

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
	db := &kernel.DB{Pool: pool}
	db.Slots = make(chan struct{}, config.MaxConns-1)
	return newStore(db), nil
}

func (s *Store) Close()                         { s.Pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

// Migrate serializes concurrent runners and checks applied migration checksums.
// Each file and its bookkeeping commit atomically; no destructive down command.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.Pool.Acquire(ctx)
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
		if err := s.Pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", file.Name()).Scan(&checksum); err != nil {
			return fmt.Errorf("database schema is not ready; run pcas migrate")
		}
		if checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
			return fmt.Errorf("migration checksum mismatch: %s", file.Name())
		}
	}
	return nil
}

var _ memory.Repository = (*Store)(nil)

// newStore composes the store over its core. Each storage package works on
// the same core; a package that needs another one holds it.
func newStore(db *kernel.DB) *Store {
	activityPart := &activitystore.Activity{DB: db}
	ownersPart := &ownerstore.Owners{DB: db}
	budgetsPart := &budgetstore.Budgets{DB: db, Owners: ownersPart}
	usagePart := &usagelog.Usage{DB: db}
	callsPart := &callstore.Calls{DB: db, Usage: usagePart, Budgets: budgetsPart}
	sourcesPart := &sourcestore.Sources{DB: db}
	claimsPart := &claimstore.Claims{DB: db, Sources: sourcesPart}
	evidencePart := &evidencestore.Evidence{DB: db}
	readerPart := &memoryread.Reader{DB: db}
	workPart := &workstore.Work{DB: db}
	intakePart := &intakestore.Intake{DB: db, Sources: sourcesPart, Owners: ownersPart, Calls: callsPart, Claims: claimsPart, Reader: readerPart, Work: workPart}
	interpPart := &interpstore.Interp{DB: db, Budgets: budgetsPart, Owners: ownersPart}
	itemsPart := &itemstore.Items{DB: db}
	memoryPart := &memorystore.Memory{DB: db}
	schedulesPart := &schedulestore.Schedules{DB: db}
	statePart := &statestore.State{DB: db, Reader: readerPart}
	workspacePart := &workspacestore.Workspace{DB: db, Claims: claimsPart, Intake: intakePart, Reader: readerPart, Owners: ownersPart, Sources: sourcesPart, State: statePart}
	return &Store{
		DB:        db,
		Interp:    interpPart,
		Workspace: workspacePart,
		Intake:    intakePart,
		Calls:     callsPart,
		State:     statePart,
		Evidence:  evidencePart,
		Work:      workPart,
		Budgets:   budgetsPart,
		Claims:    claimsPart,
		Sources:   sourcesPart,
		Items:     itemsPart,
		Owners:    ownersPart,
		Activity:  activityPart,
		Schedules: schedulesPart,
		Usage:     usagePart,
		Memory:    memoryPart,
		Reader:    readerPart,
	}
}
