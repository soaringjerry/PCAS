package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Exercise the production overlap: scheduling, result writes, and foreground
// traffic run on independent connections. No SQL locks stand in for a handler.

func phase25B3ConcurrentStatus(t *testing.T, rebuilding bool) {
	t.Helper()
	const groups = 24
	const requestBudget = 3 * time.Second
	f := phase25B234NewFixture(t)
	var refs []memory.Ref
	for i := 0; i < groups; i++ {
		g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", fmt.Sprintf("虚构并发主题%02d", i))), Name: fmt.Sprintf("虚构并发主题%02d", i), Type: "topic"}
		var members []memory.Ref
		for j := 0; j < 3; j++ {
			r := f.claim(t, fmt.Sprintf("虚构并发主题%02d便签%d：先写结论。", i, j))
			f.labels(t, r, "progress", true, 1, g)
			members = append(members, r)
		}
		refs = append(refs, members...)
		if rebuilding {
			f.card(t, g, members, true)
		}
	}
	agent := f.answerAgent(t)
	userStore, err := postgres.Open(f.ctx, f.db.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(userStore.Close)
	userFixture := *f
	userFixture.store = userStore
	userFixture.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
		return phase25B4ReplyJSON(phase25B4Reply{text: "虚构并发办事回复。"}, false)
	})
	firstModel := make(chan struct{})
	var first sync.Once
	f.model(t, func(r *http.Request, _ int, input phase25B234ModelRequest) phase25B234ModelReply {
		if card, err := phase25B3CardInput(input); err == nil {
			first.Do(func() { close(firstModel) })
			// Local latency creates ample real overlap without holding a SQL lock
			// or relying on private write order to manufacture a deadlock.
			select {
			case <-time.After(80 * time.Millisecond):
				return phase25B3JSON(phase25B3All(card))
			case <-r.Context().Done():
				return phase25B234ModelReply{status: http.StatusServiceUnavailable}
			}
		}
		body, err := phase25B3HandoverJSON(nil, nil)
		if err != nil {
			return phase25B234ModelReply{status: http.StatusInternalServerError}
		}
		return phase25B234ModelReply{content: body}
	})
	f.index(t)
	// Age only setup data. All concurrent calls below use real wall time;
	// no queue deletions, due-time rewrites, or clock jumps run with the worker.
	f.exec(t, `UPDATE memory_records SET updated_at=now()-interval '20 minutes' WHERE owner_id=$1`, f.scope.OwnerID)
	f.exec(t, `UPDATE record_versions SET recorded_at=now()-interval '20 minutes' WHERE owner_id=$1`, f.scope.OwnerID)
	f.exec(t, `UPDATE status_cards SET built_at=now()-interval '20 minutes' WHERE owner_id=$1`, f.scope.OwnerID)
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1`, f.scope.OwnerID)

	type request struct {
		name string
		run  func(context.Context) error
	}
	requests := []request{
		{"snapshot", func(ctx context.Context) error {
			_, err := userStore.Snapshot(ctx, f.scope)
			return err
		}},
		{"command", func(ctx context.Context) error {
			state, err := userStore.Snapshot(ctx, f.scope)
			if err != nil {
				return err
			}
			_, err = userStore.Execute(ctx, f.scope, workspace.Command{Type: "addTask", Kind: "task", Title: "虚构并发普通事项", Text: "虚构并发普通事项", Status: "todo", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
			return err
		}},
		{"secretary", func(ctx context.Context) error {
			turn, err := userStore.DeskTurn(postgres.WithMemoryTier(ctx, "light"), f.scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), Text: "处理虚构普通事项", AgentID: agent})
			if err == nil && turn.Turn.Reply != "虚构并发办事回复。" {
				return fmt.Errorf("unexpected secretary reply %q", turn.Turn.Reply)
			}
			return err
		}},
	}
	// Positive controls distinguish lock contention from a broken fake provider
	// or a foreground endpoint that was already failing before scheduling.
	for _, request := range requests {
		ctx, cancel := context.WithTimeout(f.ctx, requestBudget)
		start := time.Now()
		err := request.run(ctx)
		cancel()
		if err != nil {
			t.Fatalf("foreground control %s: %v", request.name, err)
		}
		t.Logf("foreground control %s=%s", request.name, time.Since(start))
	}
	f.index(t)
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1`, f.scope.OwnerID)

	runCtx, stop := context.WithTimeout(f.ctx, 25*time.Second)
	defer stop()
	var processing, schedules, overlap, cards, handovers atomic.Int32
	var mu sync.Mutex
	var failures []string
	latencies := map[string]time.Duration{}
	userCalls := map[string]int{}
	userOverlap := map[string]int{}
	problem := func(where string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if len(failures) >= 20 {
			return
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			failures = append(failures, fmt.Sprintf("%s: SQLSTATE=%s %v", where, pg.Code, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: %v", where, err))
		}
	}
	pause := func() {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-runCtx.Done():
		}
	}
	backgroundDone := make(chan struct{})
	userDone := make(chan struct{})
	var goroutines sync.WaitGroup
	goroutines.Add(3)
	go func() {
		defer goroutines.Done()
		for runCtx.Err() == nil {
			schedules.Add(1)
			if processing.Load() != 0 {
				overlap.Add(1)
			}
			if _, err := f.store.ScheduleStatus(runCtx, time.Now()); err != nil && runCtx.Err() == nil {
				problem("scheduler", err)
			}
			pause()
		}
	}()
	go func() {
		defer goroutines.Done()
		for runCtx.Err() == nil {
			job, err := f.store.Claim(runCtx, time.Minute)
			if err != nil {
				if runCtx.Err() == nil {
					problem("claim", err)
				}
				pause()
				continue
			}
			if job == nil {
				pause()
				continue
			}
			if strings.HasPrefix(job.Stage, "memory.card:") || strings.HasPrefix(job.Stage, "memory.handover:") {
				processing.Store(1)
			}
			switch {
			case strings.HasPrefix(job.Stage, "memory.card:"):
				err = f.store.ProcessCard(runCtx, *job)
				if err == nil {
					cards.Add(1)
				}
			case strings.HasPrefix(job.Stage, "memory.handover:"):
				err = f.store.ProcessHandover(runCtx, *job)
				if err == nil {
					handovers.Add(1)
				}
			case job.Stage == "memory.index":
				err = f.store.ProcessIndex(runCtx, *job)
			default:
				// A real secretary turn also queues ingestion/extraction work.
				// Keep those jobs, using the public lease/deferral boundary; this
				// acceptance case processes only status work and local indexing.
				err = f.store.Defer(runCtx, *job, "acceptance_non_status", time.Now().Add(time.Hour), true)
			}
			processing.Store(0)
			if err != nil && runCtx.Err() == nil {
				// Same rule as the worker: an error carrying a time is a deferral,
				// not a failure; completion is asserted below.
				var deferred *worker.JobError
				if errors.As(err, &deferred) && !deferred.Until.IsZero() {
					if err := f.store.Defer(runCtx, *job, deferred.Code, deferred.Until, deferred.NoAttempt); err != nil {
						problem("defer", err)
					}
				} else {
					problem("worker "+job.Stage, err)
					if err := f.store.Retry(runCtx, *job, "acceptance_processing_error"); err != nil {
						problem("retry", err)
					}
				}
			}
		}
	}()
	go func() {
		defer goroutines.Done()
		defer close(userDone)
		select {
		case <-firstModel:
		case <-runCtx.Done():
			return
		}
		for round := 0; runCtx.Err() == nil; round++ {
			for _, request := range requests {
				concurrent := processing.Load() != 0
				ctx, cancel := context.WithTimeout(runCtx, requestBudget)
				start := time.Now()
				err := request.run(ctx)
				elapsed := time.Since(start)
				cancel()
				mu.Lock()
				userCalls[request.name]++
				if concurrent {
					userOverlap[request.name]++
				}
				if elapsed > latencies[request.name] {
					latencies[request.name] = elapsed
				}
				mu.Unlock()
				if err != nil {
					problem("foreground "+request.name, err)
				}
				if elapsed > requestBudget+250*time.Millisecond {
					problem("foreground "+request.name, fmt.Errorf("elapsed %s exceeds %s", elapsed, requestBudget))
				}
			}
			if round >= 3 {
				select {
				case <-backgroundDone:
					return
				default:
				}
			}
			pause()
		}
	}()
	joined := make(chan struct{})
	go func() { goroutines.Wait(); close(joined) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("concurrent acceptance goroutines did not stop")
		}
	})
	finished := false
	for runCtx.Err() == nil {
		if !finished && cards.Load() >= groups && handovers.Load() > 0 {
			close(backgroundDone)
			finished = true
		}
		if finished {
			select {
			case <-userDone:
				stop()
			default:
			}
		}
		pause()
	}
	stop()
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent acceptance goroutines did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("schedules=%d scheduling_during_processing=%d cards=%d handovers=%d user_calls=%v user_overlap=%v max_latency=%v", schedules.Load(), overlap.Load(), cards.Load(), handovers.Load(), userCalls, userOverlap, latencies)
	for _, failure := range failures {
		t.Error(failure)
	}
	if !finished || cards.Load() != groups || handovers.Load() != 1 {
		t.Errorf("background work incomplete: cards=%d/%d handovers=%d", cards.Load(), groups, handovers.Load())
	}
	if overlap.Load() == 0 {
		t.Error("scheduler never ran during task processing")
	}
	for _, request := range requests {
		if userCalls[request.name] < 4 || userOverlap[request.name] == 0 {
			t.Errorf("foreground %s did not run repeatedly during background work", request.name)
		}
	}
	var fresh, pending int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM status_cards WHERE owner_id=$1 AND NOT stale AND rule=$2`, f.scope.OwnerID, postgres.CardVersion).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND (stage LIKE 'memory.card:%' OR stage LIKE 'memory.handover:%') AND state<>'done'`, f.scope.OwnerID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if fresh != groups || pending != 0 || f.usage(t, "card") != groups || f.usage(t, "handover") != 1 {
		t.Errorf("final fresh=%d pending=%d card_usage=%d handover_usage=%d", fresh, pending, f.usage(t, "card"), f.usage(t, "handover"))
	}
	f.assertRevisions(t, refs...)
}
