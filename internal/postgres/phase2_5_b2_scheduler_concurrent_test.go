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
func TestPhase25B2_ConcurrentCompareSchedulerWorkerAndUserRequests(t *testing.T) {
	phase25B12ConcurrentScheduler(t, "compare")
}

// Shared by the organize and compare cases. The first fake call stays active
// until one user round finishes, so even a short two-batch organize backlog
// really overlaps all three foreground operations. No database locks are forged.
func phase25B12ConcurrentScheduler(t *testing.T, kind string) {
	t.Helper()
	const requestBudget = 3 * time.Second
	f := phase25B234NewFixture(t)
	var refs []memory.Ref
	var column, stagePattern string
	var version int
	if kind == "organize" {
		column, stagePattern, version = "organized", "memory.organize:%", postgres.OrganizeVersion
		for i := 0; i < 80; i++ {
			refs = append(refs, f.claim(t, fmt.Sprintf("虚构并发整理便签%03d：一次性的演练记录。", i)))
		}
	} else {
		column, stagePattern, version = "compared", "memory.compare:%", postgres.CompareVersion
		f.sharedSubject = true
		for i := 0; i < 12; i++ {
			g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", fmt.Sprintf("虚构并发比较主题%02d", i))), Type: "topic"}
			f.subject = f.entity(t, "person", fmt.Sprintf("FictitiousConcurrentSpeaker%02d", i))
			for j := 0; j < 3; j++ {
				r := f.claim(t, fmt.Sprintf("虚构并发比较主题%02d便签%d：原始演练记录。", i, j))
				f.labels(t, r, "progress", true, 1, g)
				refs = append(refs, r)
			}
		}
	}
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = string(ref.ID)
	}
	progress := func(ctx context.Context) (int, int, error) {
		var marked, pending int
		if err := f.db.QueryRow(ctx, "SELECT count(*) FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND "+column+"=$3 AND retired=''", f.scope.OwnerID, ids, version).Scan(&marked); err != nil {
			return 0, 0, err
		}
		entityPattern := "unused:%"
		if kind == "compare" {
			entityPattern = "memory.entity_compare:%"
		}
		err := f.db.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND (stage LIKE $2 OR stage LIKE $3) AND state<>'done'`, f.scope.OwnerID, stagePattern, entityPattern).Scan(&pending)
		if err == nil && kind == "compare" {
			// Participation flags can complete before every person's and topic's
			// batch. An empty queue between scheduling rounds is not full coverage.
			var covered int
			err = f.db.QueryRow(ctx, "SELECT count(*) FROM memory_comparison_batches WHERE owner_id=$1 AND completed_at IS NOT NULL", f.scope.OwnerID).Scan(&covered)
			pending += max(0, 24-covered)
		}
		return marked, pending, err
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
	firstModel, firstUserRound := make(chan struct{}), make(chan struct{})
	var first sync.Once
	organizeJSON := phase25B1ModelJSON(t, phase25B1Items(40, "event", false))
	f.model(t, func(r *http.Request, _ int, input phase25B234ModelRequest) phase25B234ModelReply {
		initial := false
		first.Do(func() { initial = true; close(firstModel) })
		if initial {
			select {
			case <-firstUserRound:
			case <-r.Context().Done():
				return phase25B234ModelReply{status: http.StatusServiceUnavailable}
			case <-time.After(12 * time.Second):
				return phase25B234ModelReply{status: http.StatusServiceUnavailable}
			}
		}
		select {
		case <-time.After(80 * time.Millisecond):
		case <-r.Context().Done():
			return phase25B234ModelReply{status: http.StatusServiceUnavailable}
		}
		if kind == "organize" {
			return phase25B234ModelReply{content: organizeJSON}
		}
		entities, err := phase25B2Entities(input)
		if err != nil {
			return phase25B234ModelReply{status: http.StatusBadRequest}
		}
		if len(entities) > 0 {
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		return phase25B2JSON(phase25B2Empty())
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
	var processing, schedules, overlap, completed atomic.Int32
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
			var err error
			if kind == "organize" {
				_, err = f.store.ScheduleOrganize(runCtx, time.Now())
			} else {
				_, err = f.store.ScheduleCompare(runCtx, time.Now())
			}
			if err != nil && runCtx.Err() == nil {
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
			matched := kind == "organize" && strings.HasPrefix(job.Stage, "memory.organize:") || kind == "compare" && (strings.HasPrefix(job.Stage, "memory.compare:") || strings.HasPrefix(job.Stage, "memory.entity_compare:"))
			if matched {
				processing.Store(1)
			}
			switch {
			case kind == "organize" && strings.HasPrefix(job.Stage, "memory.organize:"):
				err = f.store.ProcessOrganize(runCtx, *job)
			case kind == "compare" && strings.HasPrefix(job.Stage, "memory.compare:"):
				err = f.store.ProcessCompare(runCtx, *job)
			case kind == "compare" && strings.HasPrefix(job.Stage, "memory.entity_compare:"):
				err = f.store.ProcessEntityCompare(runCtx, *job)
			case job.Stage == "memory.index":
				err = f.store.ProcessIndex(runCtx, *job)
			default:
				err = f.store.Defer(runCtx, *job, "acceptance_other_pipeline", time.Now().Add(time.Hour), true)
			}
			if err == nil && matched {
				completed.Add(1)
			}
			processing.Store(0)
			if err != nil && runCtx.Err() == nil {
				problem("worker "+job.Stage, err)
				var deferred *worker.JobError
				if errors.As(err, &deferred) && !deferred.Until.IsZero() {
					if err := f.store.Defer(runCtx, *job, deferred.Code, deferred.Until, deferred.NoAttempt); err != nil {
						problem("defer", err)
					}
				} else {
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
			if round == 0 {
				close(firstUserRound)
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
		if !finished && completed.Load() > 0 {
			marked, pending, err := progress(runCtx)
			if err != nil && runCtx.Err() == nil {
				problem("completion check", err)
			}
			if err == nil && marked == len(refs) && pending == 0 {
				close(backgroundDone)
				finished = true
			}
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
	t.Logf("pipeline=%s schedules=%d scheduling_during_processing=%d completed_jobs=%d user_calls=%v user_overlap=%v max_latency=%v", kind, schedules.Load(), overlap.Load(), completed.Load(), userCalls, userOverlap, latencies)
	for _, failure := range failures {
		t.Error(failure)
	}
	if !finished {
		t.Error("background work did not complete within 25 seconds")
	}
	if overlap.Load() == 0 {
		t.Error("scheduler never ran during task processing")
	}
	for _, request := range requests {
		if userCalls[request.name] < 4 || userOverlap[request.name] == 0 {
			t.Errorf("foreground %s did not run repeatedly during background work", request.name)
		}
	}
	marked, pending, err := progress(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	purpose := kind
	if f.usage(t, purpose) == 0 || marked != len(refs) || pending != 0 {
		t.Errorf("final marked=%d/%d pending=%d model_usage=%d", marked, len(refs), pending, f.usage(t, purpose))
	}
	f.assertRevisions(t, refs...)
}
