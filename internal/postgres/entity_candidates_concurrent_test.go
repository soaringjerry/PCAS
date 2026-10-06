package postgres_test

import (
	"context"
	"encoding/json"
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

// Run the new catalogue stage and real pair merges under the same foreground
// traffic as the existing pipelines. The first catalogue call is a barrier,
// so snapshot, command and secretary all overlap a real model invocation.
func TestEntityCandidatesConcurrentSchedulerWorkerAndUserRequests(t *testing.T) {
	t.Helper()
	const requestBudget = 3 * time.Second
	f := phase25B234NewFixture(t)
	var refs []memory.Ref
	pairs := [][2]string{{"蓝沙湾", "Azure Quay"}, {"青松港", "Pine Harbor"}, {"紫竹岛", "Violet Isle"}, {"白鹭山", "Egret Peak"}, {"金叶湖", "Golden Lake"}, {"赤霞城", "Scarlet City"}}
	partners := map[string]string{}
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构并发地理档案")), Type: "topic"}
	f.sharedSubject = true
	for _, pair := range pairs {
		partners[pair[0]], partners[pair[1]] = pair[1], pair[0]
		for _, name := range pair {
			f.subject = f.entity(t, "place", name)
			r := f.claim(t, "虚构并发地理便签："+name+"的演练记录。")
			f.labels(t, r, "progress", true, postgres.OrganizeVersion, g, workspace.MemoryGroup{EntityID: string(f.subject), Type: "place"})
			refs = append(refs, r)
		}
	}
	var catalogueCalls, confirmationCalls atomic.Int32
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = string(ref.ID)
	}
	progress := func(ctx context.Context) (int, int, error) {
		var merges, pending int
		if err := f.db.QueryRow(ctx, "SELECT count(*) FROM entity_merges WHERE owner_id=$1 AND undone_at IS NULL", f.scope.OwnerID).Scan(&merges); err != nil {
			return 0, 0, err
		}
		err := f.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND (stage LIKE 'memory.entity_candidates:%' OR stage LIKE 'memory.entity_compare:%' OR stage LIKE 'memory.compare:%') AND (state='leased' OR (state='queued' AND available_at<=now()))) + (SELECT count(*) FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND compared<$3)`, f.scope.OwnerID, ids, postgres.CompareVersion).Scan(&pending)
		return merges, pending, err
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
	f.model(t, func(r *http.Request, _ int, input phase25B234ModelRequest) phase25B234ModelReply {
		text, err := phase25B3Prompt(input)
		if err != nil {
			return phase25B234ModelReply{status: http.StatusBadRequest}
		}
		var list struct {
			Scope    string                 `json:"scope"`
			Entities []phase25B2InputEntity `json:"entities"`
		}
		if err := json.Unmarshal([]byte(text), &list); err != nil {
			return phase25B234ModelReply{status: http.StatusBadRequest}
		}
		initial := false
		if list.Scope != "" {
			first.Do(func() { initial = true; close(firstModel) })
		}
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
		if list.Scope != "" {
			catalogueCalls.Add(1)
			groups := [][]int{}
			for i, a := range list.Entities {
				for _, b := range list.Entities[i+1:] {
					if partners[a.Name] == b.Name {
						groups = append(groups, []int{a.N, b.N})
					}
				}
			}
			data, _ := json.Marshal(map[string]any{"groups": groups})
			return phase25B234ModelReply{content: string(data)}
		}
		if len(list.Entities) == 2 {
			confirmationCalls.Add(1)
			if partners[list.Entities[0].Name] == list.Entities[1].Name {
				return phase25B234ModelReply{content: `{"same":true,"keep":1}`}
			}
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		return phase25B2JSON(phase25B2Empty())
	})
	// Existing card rows stay frozen even during alias work.
	frozenGroup, frozenRefs := f.cardGroup(t, 3)
	f.card(t, frozenGroup, frozenRefs, false)
	f.index(t)
	// Create real card rows before overlap, so actual merge invalidation triggers
	// exercise the cards-before-jobs order rather than an empty-card fast path.
	if _, err := f.store.ScheduleStatus(f.ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var cards, deadlocksBefore int
	if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM status_cards WHERE owner_id=$1", f.scope.OwnerID).Scan(&cards); err != nil || cards == 0 {
		t.Fatal("card precondition", cards, err)
	}
	if err := f.db.QueryRow(f.ctx, "SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()").Scan(&deadlocksBefore); err != nil {
		t.Fatal(err)
	}
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
			_, err := userStore.Execute(ctx, f.scope, workspace.Command{Type: "addTask", Kind: "task", Title: "虚构并发普通事项", Text: "虚构并发普通事项", Status: "todo", RequestID: string(memory.NewID())})
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
			_, err := f.store.ScheduleEntityCandidates(runCtx, time.Now())
			if err == nil {
				_, err = f.store.ScheduleCompare(runCtx, time.Now())
			}
			if err != nil && runCtx.Err() == nil {
				var busy *worker.JobError
				if !errors.As(err, &busy) || busy.Code != "background_write_busy" {
					problem("scheduler", err)
				}
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
			matched := strings.HasPrefix(job.Stage, "memory.entity_candidates:") || strings.HasPrefix(job.Stage, "memory.entity_compare:") || strings.HasPrefix(job.Stage, "memory.compare:")
			if matched {
				processing.Store(1)
			}
			switch {
			case strings.HasPrefix(job.Stage, "memory.entity_candidates:"):
				err = f.store.ProcessEntityCandidates(runCtx, *job)
			case strings.HasPrefix(job.Stage, "memory.compare:"):
				err = f.store.ProcessCompare(runCtx, *job)
			case strings.HasPrefix(job.Stage, "memory.entity_compare:"):
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
				var deferred *worker.JobError
				if errors.As(err, &deferred) && deferred.NoAttempt && !deferred.Until.IsZero() {
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
				if elapsed > requestBudget {
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
			if err == nil && marked == len(pairs) && pending == 0 {
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
	t.Logf("pipeline=%s schedules=%d scheduling_during_processing=%d completed_jobs=%d user_calls=%v user_overlap=%v max_latency=%v", "entity_candidates", schedules.Load(), overlap.Load(), completed.Load(), userCalls, userOverlap, latencies)
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
	if catalogueCalls.Load() == 0 || confirmationCalls.Load() < int32(len(pairs)) || marked != len(pairs) || pending != 0 {
		t.Errorf("final merges=%d/%d pending=%d catalogue_calls=%d confirmation_calls=%d", marked, len(pairs), pending, catalogueCalls.Load(), confirmationCalls.Load())
	}
	var deadlocksAfter, changedCards int
	if err := f.db.QueryRow(f.ctx, "SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()").Scan(&deadlocksAfter); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.card:%'", f.scope.OwnerID).Scan(&changedCards); err != nil {
		t.Fatal(err)
	}
	if deadlocksAfter != deadlocksBefore || changedCards != 0 {
		t.Errorf("deadlocks before=%d after=%d unexpected card jobs=%d", deadlocksBefore, deadlocksAfter, changedCards)
	}
	t.Logf("catalogue_calls=%d confirmation_calls=%d frozen_card_jobs=%d deadlocks=%d", catalogueCalls.Load(), confirmationCalls.Load(), changedCards, deadlocksAfter-deadlocksBefore)
	f.assertRevisions(t, refs...)
}
