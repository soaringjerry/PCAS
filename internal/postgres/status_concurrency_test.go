package postgres

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Pause the result transaction after lease fencing, on a separate worker pool.
// This widens the production interleaving without forging jobs or lock order.
type statusResultBarrier struct {
	reads            atomic.Int32
	entered, release chan struct{}
}

func (b *statusResultBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, q pgx.TraceQueryStartData) context.Context {
	if q.SQL == "SELECT available_at FROM memory_jobs WHERE id=$1" && b.reads.Add(1) == 2 {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*statusResultBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestStatusConcurrentScheduleBuildAndUserRequests(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 12)
	for i := range 3 {
		compareEntityGroup(t, s, scope, "topic", fmt.Sprintf("虚构并发主题%d", i), refs[:3]...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs WHERE stage NOT LIKE 'memory.card:%'"); err != nil {
		t.Fatal(err)
	}
	b := &statusResultBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	config := s.pool.Config()
	config.ConnConfig.Tracer = b
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	workerStore := &Store{pool: pool, models: s.models}
	var before int
	if err := s.pool.QueryRow(ctx, "SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	workerDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(workerDone)
		for range 5 {
			j, err := workerStore.Claim(ctx, time.Minute)
			if err != nil || j == nil {
				errors <- fmt.Errorf("claim: %v, %v", j, err)
				return
			}
			if err := workerStore.ProcessCard(ctx, *j); err != nil {
				errors <- fmt.Errorf("build %s: %w", j.Stage, err)
				return
			}
		}
	}()
	select {
	case <-b.entered:
	case <-ctx.Done():
		t.Fatal("worker did not reach result transaction")
	}
	scheduleCtx, stopSchedule := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for scheduleCtx.Err() == nil {
			if _, err := s.ScheduleStatus(scheduleCtx, time.Now()); err != nil && scheduleCtx.Err() == nil {
				// Short lock contention is retried; deadlocks are never accepted.
				if failure, ok := err.(*worker.JobError); !ok || failure.Code != "background_write_busy" {
					errors <- fmt.Errorf("schedule: %w", err)
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		requestCtx, done := context.WithTimeout(ctx, 300*time.Millisecond)
		_, err := s.Snapshot(requestCtx, scope)
		done()
		if err != nil {
			errors <- fmt.Errorf("snapshot timed out during result write: %w", err)
		}
		requestCtx, done = context.WithTimeout(ctx, 2*time.Second)
		_, err = s.Execute(requestCtx, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"city": "虚构城"}), RequestID: string(memory.NewID())})
		done()
		if err != nil {
			errors <- fmt.Errorf("command timed out during result write: %w", err)
		}
	}()
	// Keep the result write open longer than the snapshot deadline. It must
	// coexist with FOR SHARE, while a command may wait for this short write.
	time.Sleep(500 * time.Millisecond)
	close(b.release)
	var built int
	for ctx.Err() == nil {
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM status_cards WHERE built_at IS NOT NULL").Scan(&built); err != nil {
			errors <- err
			break
		}
		if built == 5 {
			break
		}
		select {
		case <-workerDone:
			stopSchedule()
		default:
		}
		if scheduleCtx.Err() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopSchedule()
	wg.Wait()
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM status_cards WHERE built_at IS NOT NULL").Scan(&built); err != nil {
		t.Error(err)
	}
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if built != 5 {
		t.Errorf("built %d cards, want 5", built)
	}
	var after int
	if err := s.pool.QueryRow(context.Background(), "SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("database detected %d deadlocks", after-before)
	}
}
