package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Work that a handler queues under its lease keeps the root of the handler's
// job and is one step deeper. Each new piece of queued work is one event.
func TestQueuedWorkKeepsItsRootAndDepthAndIsRecordedOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	source := b1Source(t, s, scope, "虚构的记录", "虚构的云杉喜欢茶。", "manual")
	j, err := s.Claim(ctx, time.Minute)
	if err != nil || j == nil {
		t.Fatal("no job to claim", err)
	}
	type row struct {
		root, cause *string
		depth       int
	}
	read := func(id string) row {
		var r row
		if err := s.pool.QueryRow(ctx, "SELECT root_job_id::text,cause_job_id::text,chain_depth FROM memory_jobs WHERE id=$1", id).Scan(&r.root, &r.cause, &r.depth); err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := read(string(j.ID))
	if first.root == nil || *first.root != string(j.ID) || first.cause != nil || first.depth != 0 {
		t.Fatalf("work that a request queued starts a chain: %+v", first)
	}
	// A handler queues follow-on work under its lease.
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, *j); err != nil {
			return err
		}
		return enqueue(ctx, tx, j.OwnerID, source.ID, source.Version, "fictitious.child")
	}); err != nil {
		t.Fatal(err)
	}
	var child string
	if err := s.pool.QueryRow(ctx, "SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND stage='fictitious.child'", string(scope.OwnerID)).Scan(&child); err != nil {
		t.Fatal(err)
	}
	got := read(child)
	if got.root == nil || *got.root != string(j.ID) || got.cause == nil || *got.cause != string(j.ID) || got.depth != 1 {
		t.Fatalf("%+v", got)
	}
	var events, fromJob int
	if err := s.pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER (WHERE trigger_source='job' AND chain_depth=1 AND root_id=$2) FROM recorded_events WHERE owner_id=$1 AND job_id=$3", string(scope.OwnerID), string(j.ID), child).Scan(&events, &fromJob); err != nil || events != 1 || fromJob != 1 {
		t.Fatal(events, fromJob, err)
	}
	// Repeated delivery changes the existing job and adds no event.
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return enqueue(ctx, tx, j.OwnerID, source.ID, source.Version, "fictitious.child")
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM recorded_events WHERE job_id=$1", child).Scan(&events); err != nil || events != 1 {
		t.Fatal("repeated delivery added an event", events, err)
	}
	// A rollback leaves neither the work nor its event.
	rolledBack := errors.New("fictitious rollback")
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := enqueue(ctx, tx, j.OwnerID, source.ID, source.Version, "fictitious.rolled-back"); err != nil {
			return err
		}
		return rolledBack
	})
	if !errors.Is(err, rolledBack) {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM recorded_events WHERE owner_id=$1 AND stage LIKE 'fictitious.%'", string(scope.OwnerID)).Scan(&events); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'fictitious.%'", string(scope.OwnerID)).Scan(&jobs); err != nil || jobs != 1 || events != 1 {
		t.Fatal("a rollback left work or an event", jobs, events, err)
	}
}
