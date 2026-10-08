package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/worker"
)

// Each old stage is rerun with the EXTENDED phase3 fixture, real HTTP user
// traffic, and a held actual provider call, not independent sequential tests.
func phase3ConcurrentStage(t *testing.T, stage string) {
	t.Helper()
	p3 := phase3LoadFixture(t)
	f := p3.phase26LoadedFixture
	m := phase26NewModel(t, f)
	s, ctx := f.Store, f.Context
	if stage != OrganizeStage {
		phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	}
	if stage == EntityCompareStage {
		phase26Proposal(t, f, 0, 4)
	}
	user, err := Open(ctx, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(user.Close)
	user.SetModels(s.models)
	traffic := phase26HTTPUserRequests(t, f, user)
	// Warm foreground setup before the zero-write/read and overlap measurements.
	for name, run := range traffic {
		if err := run(ctx); err != nil {
			t.Fatalf("warm %s: %v", name, err)
		}
	}
	phase26Isolate(t, f, stage)
	if _, err := phase26Schedule(ctx, s, stage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, stage)
	job := phase26ClaimStage(t, f, stage)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	m.mu.Lock()
	m.Before = func(c context.Context, call phase26Call) {
		if call.Stage == stage {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-c.Done():
			}
		}
	}
	m.mu.Unlock()
	workctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- phase26Process(workctx, s, job) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("provider overlap not reached: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("provider barrier timeout")
	}
	schedulerDone := make(chan error, 1)
	go func() {
		for i := 0; i < 6; i++ {
			_, err := phase26Schedule(workctx, s, stage)
			var busy *worker.JobError
			if err != nil && !(errors.As(err, &busy) && busy.Code == "background_write_busy") {
				schedulerDone <- err
				return
			}
		}
		schedulerDone <- nil
	}()
	samples := map[string][]time.Duration{}
	for i := 0; i < 3; i++ {
		for _, name := range []string{"snapshot", "library", "command", "secretary"} {
			start := time.Now()
			err := traffic[name](workctx)
			samples[name] = append(samples[name], time.Since(start))
			if err != nil {
				t.Errorf("concurrent %s: %v", name, err)
			}
		}
	}
	if err := <-schedulerDone; err != nil {
		t.Errorf("scheduler: %v", err)
	}
	unblock()
	processErr := <-done
	// Result persistence can yield to foreground. Resume the same paid job, never
	// replace it with a newly scheduled job or discard the cached model result.
	for i := 0; i < 12 && processErr != nil; i++ {
		var busy *worker.JobError
		if !errors.As(processErr, &busy) || busy.Code != "background_write_busy" {
			break
		}
		processErr = phase26Process(workctx, s, job)
	}
	if processErr != nil {
		t.Fatal("processor", processErr)
	}
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM memory_jobs WHERE id=$1`, job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "done" || len(m.calls(stage)) != 1 {
		t.Errorf("stage=%s state=%s actual paid calls=%d", stage, state, len(m.calls(stage)))
	}
	for name, latency := range samples {
		phase3Latency(t, stage+"/concurrent/"+name, latency)
	}
	health, err := s.BackgroundHealth(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("T2 stage=%s extended_projects=30 schedules=6 paid_calls=%d health=%s", stage, len(m.calls(stage)), health)
}
func TestPhase3T2T4EachExistingStageConcurrentHTTPScale(t *testing.T) {
	for _, stage := range []string{OrganizeStage, CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage} {
		t.Run(stage, func(t *testing.T) { phase3ConcurrentStage(t, stage) })
	}
}

// This is only an adapter for implementation-defined phase names/signatures.
// It never infers expected results from the implementation. Until these stages
// exist, S-P3-001/005 remain explicit blockers; no fake scheduler certifies them.
// H adapter follows published PR #264: ScheduleStatus -> ProcessProjectHandover.
// The effort adapter must be aligned with the published P signatures when available.
func phase3StageName(t *testing.T, logical string) string {
	t.Helper()
	matches := []string{}
	for stage := range backgroundHourlyBudgets {
		if logical == "project_handover" && strings.Contains(stage, "project") && strings.Contains(stage, "handover") || logical == "effort" && strings.Contains(stage, "effort") {
			matches = append(matches, stage)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("no unambiguous implementation stage for %s: %v", logical, matches)
	}
	return matches[0]
}
func phase3ReflectCall(s *Store, name string, args ...any) ([]reflect.Value, error) {
	method := reflect.ValueOf(s).MethodByName(name)
	if !method.IsValid() {
		return nil, fmt.Errorf("phase3 backend has no %s adapter", name)
	}
	if method.Type().NumIn() != len(args) {
		return nil, fmt.Errorf("phase3 %s signature differs from adapter", name)
	}
	in := []reflect.Value{}
	for i, arg := range args {
		v := reflect.ValueOf(arg)
		if !v.Type().AssignableTo(method.Type().In(i)) {
			return nil, fmt.Errorf("phase3 %s argument %d differs", name, i)
		}
		in = append(in, v)
	}
	out := method.Call(in)
	last := out[len(out)-1]
	if !last.IsNil() {
		return out, last.Interface().(error)
	}
	return out, nil
}
func phase3Schedule(s *Store, ctx context.Context, logical string, now time.Time) error {
	name := map[string]string{"project_handover": "ScheduleStatus", "effort": "ScheduleStatus"}[logical]
	_, err := phase3ReflectCall(s, name, ctx, now)
	return err
}
func phase3Process(s *Store, ctx context.Context, logical string, job worker.Job) error {
	name := map[string]string{"project_handover": "ProcessProjectHandover", "effort": "ProcessEffort"}[logical]
	_, err := phase3ReflectCall(s, name, ctx, job)
	return err
}
func TestPhase3G1G2G3G4G5ExistingPipelineRegressions(t *testing.T) {
	// Every test below also runs on its own in this package; the wrapper is
	// kept for the acceptance record and costs ten minutes, so CI leaves it to
	// the explicit full regression run.
	if os.Getenv("PCAS_FULL_REGRESSION") == "" {
		t.Skip("set PCAS_FULL_REGRESSION=1 to rerun the 2.6 assertions as one group")
	}
	// Reuse the independently frozen 2.6 assertions, not new implementation-shaped
	// unit tests. All helpers unconditionally create their own disposable container.
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"G1_visible_overflow", TestPhase26G1C6SecretaryContextAndActionOverflowRemainVisible},
		{"G2_bad_output_preserves_pending", TestPhase26G2A5B5InvalidOutputsPreserveGoodData},
		{"G2_backoff", TestPhase26G2A5ComparisonInvalidOutputKeepsSameBatchPending},
		{"G3_paid_result_restart", TestPhase26G3A1PaidResultsSurviveWriteFailureAndRestart},
		{"G4_incremental", TestPhase26G4A6A10ComparisonCoverageAndUnchangedScheduling},
		{"G4_T4_single_memory_calls", TestPhase26G4T4OneMemoryEditRunsOnlyIts40AffectedComparisonCalls},
		{"G5_model_semantics", TestPhase26C3SemanticGroupAndDepthPiggybackPrimaryAnswer},
	} {
		t.Run(tc.name, tc.run)
	}
}

func TestPhase3G3ClassificationAndGlobalHandoverPaidResultRecovery(t *testing.T) {
	for _, tc := range []struct{ stage, table string }{{OrganizeStage, "claim_revisions"}, {HandoverStage, "handovers"}} {
		t.Run(tc.stage, func(t *testing.T) {
			p3 := phase3LoadFixture(t)
			f := p3.phase26LoadedFixture
			m := phase26NewModel(t, f)
			s, ctx := f.Store, f.Context
			if tc.stage == HandoverStage {
				phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
			}
			phase26Isolate(t, f, tc.stage)
			if _, err := phase26Schedule(ctx, s, tc.stage); err != nil {
				t.Fatal(err)
			}
			phase26Isolate(t, f, tc.stage)
			job := phase26ClaimStage(t, f, tc.stage)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			m.mu.Lock()
			m.Before = func(c context.Context, call phase26Call) {
				if call.Stage == tc.stage {
					once.Do(func() { close(entered) })
					select {
					case <-release:
					case <-c.Done():
					}
				}
			}
			m.mu.Unlock()
			done := make(chan error, 1)
			go func() { done <- phase26Process(ctx, s, job) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatal("no actual paid call", err)
			case <-time.After(20 * time.Second):
				close(release)
				t.Fatal("provider barrier")
			}
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				close(release)
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(ctx, "LOCK TABLE "+tc.table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			err = <-done
			var busy *worker.JobError
			if !errors.As(err, &busy) || busy.Code != "background_write_busy" {
				t.Fatal("paid result write was not retained/deferred", err)
			}
			var saved int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, job.ID).Scan(&saved); err != nil || saved != 1 {
				t.Fatal("paid result is not durable", saved, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			restarted, err := Open(ctx, s.pool.Config().ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			restarted.SetModels(s.models)
			if err := phase26Process(ctx, restarted, job); err != nil {
				t.Fatal(err)
			}
			if len(m.calls(tc.stage)) != 1 {
				t.Fatal("result retry recalled model", len(m.calls(tc.stage)))
			}
		})
	}
}
func TestPhase3G7ExistingStagesActualFinalSlotAndExhaustion(t *testing.T) {
	TestPhase26G7T4ActualProviderAttemptAtEachHourlyBoundary(t)
}
func TestPhase3G7AllSevenFrozenCeilingsAndGlobal140(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	expected := map[string]int{OrganizeStage: 40, CompareStage: 40, EntityCompareStage: 30, EntityCandidatesStage: 6, HandoverStage: 2, phase3StageName(t, "project_handover"): 10, phase3StageName(t, "effort"): 10}
	if !reflect.DeepEqual(backgroundHourlyBudgets, expected) {
		t.Fatal("stage ceilings differ from contract", backgroundHourlyBudgets)
	}
	if organizeCompareHourlyLimit != 140 {
		t.Fatal("global configured ceiling is not140", organizeCompareHourlyLimit)
	}
}
