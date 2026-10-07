package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestPhase26G3A1PaidResultsSurviveWriteFailureAndRestart(t *testing.T) {
	for _, tc := range []struct{ stage, table string }{{CompareStage, "memory_comparison_batches"}, {EntityCompareStage, "entity_alias_receipts"}, {EntityCandidatesStage, "background_markers"}} {
		t.Run(strings.TrimPrefix(tc.stage, "memory."), func(t *testing.T) {
			f := phase26LoadFixture(t)
			m := phase26NewModel(t, f)
			s, ctx := f.Store, f.Context
			phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
			if tc.stage == EntityCompareStage {
				phase26Proposal(t, f, 0, 4)
			}
			phase26Isolate(t, f, tc.stage)
			if _, err := phase26Schedule(ctx, s, tc.stage); err != nil {
				t.Fatal(err)
			}
			phase26Isolate(t, f, tc.stage)
			j := phase26ClaimStage(t, f, tc.stage)
			entered, release := make(chan struct{}), make(chan struct{})
			m.mu.Lock()
			m.Before = func(c context.Context, call phase26Call) {
				close(entered)
				select {
				case <-release:
				case <-c.Done():
				}
			}
			m.mu.Unlock()
			done := make(chan error, 1)
			go func() { done <- phase26Process(ctx, s, j) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("no paid provider attempt: %v", err)
			case <-time.After(15 * time.Second):
				t.Fatal("provider barrier timeout")
			}
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(ctx, "LOCK TABLE "+tc.table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			close(release)
			err = <-done
			var busy *worker.JobError
			if !errors.As(err, &busy) || busy.Code != "background_write_busy" {
				t.Errorf("result write did not defer safely: %T %v", err, err)
			}
			var persisted int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, j.ID).Scan(&persisted); err != nil {
				t.Fatal(err)
			}
			if persisted != 1 || len(m.calls(tc.stage)) != 1 {
				t.Fatalf("paid result lost before recovery saved=%d calls=%d", persisted, len(m.calls(tc.stage)))
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
			// A new Store has no in-process paid-result cache. Same real leased job.
			if err := phase26Process(ctx, restarted, j); err != nil {
				t.Fatalf("restart result-only recovery: %v", err)
			}
			var state string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM memory_jobs WHERE id=$1`, j.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if len(m.calls(tc.stage)) != 1 || state != "done" {
				t.Errorf("recovery re-called provider or did not finish calls=%d state=%s", len(m.calls(tc.stage)), state)
			}
			t.Logf("stage=%s durable_paid_results_before_restart=%d actual_provider_attempts=%d queue=%s", tc.stage, persisted, len(m.calls(tc.stage)), state)
		})
	}
}
