package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Jobs for imported history are given their lower priority when they are
// queued, so a claim finds the next job at the head of the queue: something
// said now is taken before the import's backlog, without examining it all.
func TestImportedHistoryJobsAreQueuedBehindFreshOnes(t *testing.T) {
	s, scope := b4Store(t), owner()
	ctx := context.Background()
	b4SmallChunks(t, 1)
	conversations := b4Conversations(t, "window", 40)
	id, archive := b4ImportFile(t, s, scope, "window.zip", b4Zip(t, b4Export(conversations), false), map[string]string{"organize": "now"})
	b4Complete(t, s, scope, id, archive)
	var fresh, history int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE priority<10),count(*) FILTER (WHERE priority>=10) FROM memory_jobs WHERE owner_id=$1 AND state='queued'`, string(scope.OwnerID)).Scan(&fresh, &history); err != nil {
		t.Fatal(err)
	}
	if fresh != 0 || history < 40 {
		t.Fatalf("queued import jobs: %d ahead of fresh input, %d behind it", fresh, history)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "刚说的事"})
	_ = st
	var said string
	if err := s.pool.QueryRow(ctx, `SELECT j.record_id::text FROM memory_jobs j WHERE j.owner_id=$1 AND j.state='queued' AND j.priority<10 ORDER BY j.created_at DESC LIMIT 1`, string(scope.OwnerID)).Scan(&said); err != nil {
		t.Fatal("something said now queued no job ahead of the import", err)
	}
	job, err := s.Claim(ctx, time.Minute)
	if err != nil || job == nil {
		t.Fatal("claim", err)
	}
	if string(job.Record.ID) != said {
		t.Fatalf("claimed %s (%s) before what was just said", job.Record.ID, job.Stage)
	}
	// The windowed claim and the whole-queue claim agree on what comes next.
	var windowed, whole string
	pick := func(query string) string {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var jobID, a, b, d, e, f string
		var v, n int
		if err := tx.QueryRow(ctx, query, 60.0, "11111111-1111-4111-8111-111111111111", 5).Scan(&jobID, &a, &b, &v, &d, &n, &e, &f); err != nil {
			t.Fatal(err)
		}
		return jobID
	}
	windowed, whole = pick(claimWindowed), pick(claimWhole)
	if windowed != whole {
		t.Fatalf("windowed claim took %s, whole-queue claim %s", windowed, whole)
	}
}
