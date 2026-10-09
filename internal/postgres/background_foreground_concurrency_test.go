package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase26Proposal(t *testing.T, f *phase26LoadedFixture, a, b int) {
	t.Helper()
	phase26Exec(t, f, `INSERT INTO entity_alias_candidates(owner_id,left_id,right_id,name_hash,rule,source_marker) VALUES($1,least($2::uuid,$3::uuid),greatest($2::uuid,$3::uuid),entity_name_hash($1,$2,$3),2,'fictitious model proposal') ON CONFLICT DO NOTHING`, f.Scope.OwnerID, f.Entities[a], f.Entities[b])
}
func phase26UserRequests(f *phase26LoadedFixture, s *Store) map[string]func(context.Context) error {
	useSyntheticGateway(s)
	return map[string]func(context.Context) error{
		"snapshot": func(ctx context.Context) error { _, err := s.Snapshot(ctx, f.Scope); return err },
		"library": func(ctx context.Context) error {
			_, err := s.ListGroupMemories(ctx, f.Scope, "entity:"+string(f.Entities[0]), workspace.GroupMemoryQuery{Limit: 50})
			return err
		},
		"command": func(ctx context.Context) error {
			_, err := s.Execute(ctx, f.Scope, workspace.Command{Type: "addTask", Title: "Fictitious concurrent task", Text: "Fictitious concurrent task", RequestID: string(memory.NewID())})
			return err
		},
		"secretary": func(ctx context.Context) error {
			out, err := s.DeskTurn(WithMemoryTier(ctx, "light"), f.Scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase26", Text: "Please answer this fictitious ordinary question."})
			if err == nil && out.Turn.Reply != "Fictitious acceptance reply." {
				return fmt.Errorf("unexpected secretary response %q", out.Turn.Reply)
			}
			return err
		},
	}
}
func phase26ConcurrentCase(t *testing.T, stage string, merge bool, overHTTP ...bool) {
	t.Helper()
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	s := f.Store
	if stage != OrganizeStage {
		phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	}
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if merge {
		// Give the destination 1300 distinct members. The source has 1200 and
		// all its links must move even when the keeper policy prefers counts.
		phase26Exec(t, f, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) SELECT $1,x,1,$3,'person' FROM unnest($2::uuid[]) x ON CONFLICT DO NOTHING`, f.Scope.OwnerID, f.Claims[:1300], f.Entities[4])
	}
	if stage == EntityCompareStage {
		phase26Proposal(t, f, 0, 4)
	}
	if merge {
		m.Reply = func(call phase26Call) string {
			if call.Stage != EntityCompareStage {
				return m.goldReply(call)
			}
			var p struct {
				Entities []struct {
					N    int
					Name string
				}
			}
			_ = json.Unmarshal([]byte(call.Prompt), &p)
			for _, e := range p.Entities {
				if e.Name == f.Corpus.Entities[4].Name {
					return fmt.Sprintf(`{"same":true,"keep":%d}`, e.N)
				}
			}
			return `{"same":false,"keep":null}`
		}
	}
	phase26Isolate(t, f, stage)
	userStore, err := Open(f.Context, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(userStore.Close)
	userStore.SetModels(s.models)
	requests := phase26UserRequests(f, userStore)
	if len(overHTTP) > 0 && overHTTP[0] {
		requests = phase26HTTPUserRequests(t, f, userStore)
	}
	baseline := map[string]time.Duration{}
	for name, run := range requests {
		ctx, cancel := context.WithTimeout(f.Context, 30*time.Second)
		start := time.Now()
		err := run(ctx)
		cancel()
		baseline[name] = time.Since(start)
		t.Logf("foreground baseline %s=%s calls=%d", name, baseline[name], len(m.calls("secretary")))
		if err != nil {
			t.Fatalf("foreground baseline %s: %v", name, err)
		}
	}
	if _, err := phase26Schedule(f.Context, s, stage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, stage)
	job := phase26ClaimStage(t, f, stage)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	m.Before = func(ctx context.Context, call phase26Call) {
		if call.Stage == stage {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	runCtx, stop := context.WithTimeout(f.Context, 100*time.Second)
	defer stop()
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		current := job
		for {
			err := phase26Process(runCtx, s, current)
			var deferErr *worker.JobError
			if !errors.As(err, &deferErr) || !deferErr.NoAttempt || deferErr.Code != "background_write_busy" {
				done <- err
				return
			}
			if err := s.Defer(runCtx, current, deferErr.Code, deferErr.Until, true); err != nil {
				done <- err
				return
			}
			select {
			case <-time.After(time.Until(deferErr.Until) + 10*time.Millisecond):
			case <-runCtx.Done():
				done <- runCtx.Err()
				return
			}
			for {
				next, err := s.Claim(runCtx, 5*time.Minute)
				if err != nil {
					done <- err
					return
				}
				if next == nil {
					select {
					case <-time.After(10 * time.Millisecond):
					case <-runCtx.Done():
						done <- runCtx.Err()
						return
					}
					continue
				}
				if next.ID == job.ID {
					current = *next
					break
				}
				if err := s.Defer(runCtx, *next, "phase26_nonfocus_pipeline", time.Now().Add(24*time.Hour), true); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("processor did not enter actual provider: %v", err)
	case <-runCtx.Done():
		t.Fatal("provider overlap barrier timed out")
	}
	var schedules atomic.Int32
	var mu sync.Mutex
	failures := []string{}
	schedulerDefers := 0
	max := map[string]time.Duration{}
	calls := map[string]int{}
	sqlstates := map[string]int{}
	report := func(where string, err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		var busy *worker.JobError
		if where == "scheduler" && errors.As(err, &busy) && busy.Code == "background_write_busy" {
			schedulerDefers++
			return
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			sqlstates[pg.Code]++
		}
		failures = append(failures, where+": "+err.Error())
	}
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		for runCtx.Err() == nil {
			_, err := phase26Schedule(runCtx, s, stage)
			if runCtx.Err() == nil {
				report("scheduler", err)
			}
			schedules.Add(1)
			select {
			case <-time.After(50 * time.Millisecond):
			case <-runCtx.Done():
			}
		}
	}()
	round := func() {
		for _, name := range []string{"snapshot", "library", "command", "secretary"} {
			ctx, cancel := context.WithTimeout(runCtx, 20*time.Second)
			start := time.Now()
			err := requests[name](ctx)
			elapsed := time.Since(start)
			cancel()
			mu.Lock()
			calls[name]++
			if elapsed > max[name] {
				max[name] = elapsed
			}
			mu.Unlock()
			report("foreground "+name, err)
		}
	}
	// Three complete foreground rounds while the REAL model call is in flight.
	for i := 0; i < 3; i++ {
		round()
	}
	releaseOnce.Do(func() { close(release) })
	var processErr error
	complete := false
	for !complete && runCtx.Err() == nil {
		round()
		select {
		case processErr = <-done:
			complete = true
		default:
		}
	}
	if !complete {
		select {
		case processErr = <-done:
		case <-time.After(time.Second):
			processErr = fmt.Errorf("processor failed to stop")
		}
	}
	elapsed := time.Since(started)
	stop()
	<-schedulerDone
	report("processor", processErr)
	var state string
	if err := s.pool.QueryRow(f.Context, `SELECT state FROM memory_jobs WHERE id=$1`, job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	health, err := s.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stage=%s merge=%t baseline=%v concurrent_max=%v foreground_calls=%v schedules=%d processor_elapsed=%s SQLSTATE=%v state=%s health=%s", stage, merge, baseline, max, calls, schedules.Load(), elapsed, sqlstates, state, health)
	t.Logf("scheduler owner-lock deferrals=%d", schedulerDefers)
	t.Run("no_repeated_owner_lock_deferrals", func(t *testing.T) {
		if stage == EntityCandidatesStage || stage == HandoverStage || merge {
			phase26Finding(t, "S-P26-002")
		}
		if schedulerDefers > 1 {
			t.Errorf("repeated owner-lock scheduler deferrals=%d", schedulerDefers)
		}
	})
	for _, failure := range failures {
		t.Error(failure)
	}
	if schedules.Load() == 0 || len(m.calls(stage)) != 1 || state != "done" {
		t.Errorf("overlap/completion/paid-call mismatch schedules=%d provider=%d state=%s", schedules.Load(), len(m.calls(stage)), state)
	}
	if stage == EntityCompareStage {
		var pending int
		if err := s.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.entity_compare:%' AND state IN('queued','leased')`, f.Scope.OwnerID).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending != 0 {
			t.Errorf("alias queue did not drain after joined scheduler/processor: %d", pending)
		}
	}
	if merge {
		var remaining, moved int
		if err := s.pool.QueryRow(f.Context, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, f.Entities[0]).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(f.Context, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, f.Entities[4]).Scan(&moved); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 || moved != 1300 || elapsed >= time.Minute {
			t.Errorf("large merge remaining=%d moved=%d elapsed=%s", remaining, moved, elapsed)
		}
	}
}
func TestBackgroundStagesAllowConcurrentForegroundScale(t *testing.T) {
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage} {
		t.Run(strings.TrimPrefix(stage, "memory."), func(t *testing.T) {
			phase26ConcurrentCase(t, stage, false)
		})
	}
}
func TestLargeEntityMergeAllowsConcurrentForegroundScale(t *testing.T) {
	phase26ConcurrentCase(t, EntityCompareStage, true)
}

// The real processors share a session call fence. A parallel attempt must
// yield without charging/attempt consumption, then succeed after the fence clears.
func TestIndependentBackgroundStagesYieldSharedCallFence(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Isolate(t, f, OrganizeStage)
	if _, err := f.Store.ScheduleOrganize(f.Context, time.Now()); err != nil {
		t.Fatal(err)
	}
	a := phase26ClaimStage(t, f, OrganizeStage)
	if _, err := f.Store.ScheduleStatus(f.Context, time.Now()); err != nil {
		t.Fatal(err)
	}
	b := phase26ClaimStage(t, f, HandoverStage)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	m.mu.Lock()
	m.Before = func(ctx context.Context, c phase26Call) {
		if c.Stage == OrganizeStage {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- phase26Process(f.Context, f.Store, a) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("no real classification call: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("provider barrier timeout")
	}
	err := phase26Process(f.Context, f.Store, b)
	var busy *worker.JobError
	if !errors.As(err, &busy) || !busy.NoAttempt || busy.Code != "status_busy" {
		t.Errorf("shared call fence consumed an attempt/charge: %v", err)
	}
	if len(m.calls(HandoverStage)) != 0 {
		t.Error("parallel fenced attempt called provider")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := phase26Process(f.Context, f.Store, b); err != nil {
		t.Fatal(err)
	}
	if len(m.calls(OrganizeStage)) != 1 || len(m.calls(HandoverStage)) != 1 {
		t.Error("fence recovery duplicated a provider call")
	}
	t.Log("all five stages share the call fence; simultaneous result-writer contention must additionally cover extraction/user writes")
}
