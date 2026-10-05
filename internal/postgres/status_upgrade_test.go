package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestStatusUpgradeExistingQueueAndRetirementTrigger(t *testing.T) {
	s, scope := b1EmptyStore(t), owner()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	// Install the exact released schema and checksums before applying the fix.
	for _, file := range files {
		if file.Name() >= "039_status_lock_order.sql" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
			return err
		}); err != nil {
			t.Fatal(file.Name(), err)
		}
	}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 3)
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET priority=13 WHERE stage LIKE 'memory.card:%'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeat migration", err)
	}
	var selfPriority, otherPriority int
	if err := s.pool.QueryRow(ctx, "SELECT min(priority) FILTER(WHERE stage LIKE '%:self:%'),min(priority) FILTER(WHERE stage NOT LIKE '%:self:%') FROM memory_jobs WHERE stage LIKE 'memory.card:%'").Scan(&selfPriority, &otherPriority); err != nil {
		t.Fatal(err)
	}
	if selfPriority != SelfCardPriority || otherPriority != CardPriority {
		t.Fatal(selfPriority, otherPriority)
	}
	for range 2 {
		if err := s.ProcessCard(ctx, statusTestJob(t, s, scope)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[0].ID, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	var stale, debounced int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM status_cards WHERE stale").Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE stage LIKE 'memory.card:%' AND state='queued' AND available_at>now()+interval '9 minutes'").Scan(&debounced); err != nil {
		t.Fatal(err)
	}
	if stale != 2 || debounced != 2 {
		t.Fatal("retirement failed to invalidate and debounce", stale, debounced)
	}
}
