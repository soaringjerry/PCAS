package postgres

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func backgroundFixture(t *testing.T, stage string) (*Store, memory.Scope, worker.Job, func(context.Context, worker.Job) error, func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	switch stage {
	case "compare":
		compareFixture(t, s, scope, "虚构进展甲。", "虚构进展乙。", "虚构进展丙。")
		j := compareJob(t, s, scope, false, CompareVersion)
		return s, scope, j, s.ProcessCompare, func(w http.ResponseWriter, _ *http.Request) {
			secretaryModelReply(w, map[string]any{"duplicates": []any{}, "superseded": []any{}})
		}
	case "handover":
		statusTestMemories(t, s, scope, 3)

		j := handoverTestJob(t, s, scope)
		return s, scope, j, s.ProcessHandover, func(w http.ResponseWriter, r *http.Request) { handoverFakeReply(t, w, r) }
	default:
		statusTestMemories(t, s, scope, 3)
		j := statusTestJob(t, s, scope)
		return s, scope, j, s.ProcessCard, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) }
	}
}

func TestBackgroundModelCallsLeaveOwnerUnlocked(t *testing.T) {
	for _, stage := range []string{"compare", "handover"} {
		t.Run(stage, func(t *testing.T) {
			s, scope, j, process, reply := backgroundFixture(t, stage)
			entered, release := make(chan struct{}), make(chan struct{})
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; reply(w, r) })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- process(ctx, j) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("model did not start")
			}
			requestCtx, finish := context.WithTimeout(ctx, 500*time.Millisecond)
			_, snapErr := s.Snapshot(requestCtx, scope)
			_, commandErr := s.Execute(requestCtx, scope, workspace.Command{Type: "updateSettings", RequestID: string(memory.NewID()), Patch: asJSON(map[string]string{"city": "虚构城"})})
			finish()
			close(release)
			if snapErr != nil || commandErr != nil {
				t.Errorf("owner locked over %s model call: snapshot=%v command=%v", stage, snapErr, commandErr)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBackgroundContendedHandoverYieldsOwnerPromptly(t *testing.T) {
	s, scope, j, process, reply := backgroundFixture(t, "handover")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "INSERT INTO handovers(owner_id,body,rule) VALUES($1,'虚构旧交接',1) ON CONFLICT(owner_id) DO NOTHING", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1 FROM handovers WHERE owner_id=$1 FOR UPDATE", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { reply(w, r); close(entered) })
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- process(ctx, j) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("model did not start")
	}
	requestCtx, finish := context.WithTimeout(ctx, 500*time.Millisecond)
	_, snapErr := s.Snapshot(requestCtx, scope)
	_, commandErr := s.Execute(requestCtx, scope, workspace.Command{Type: "updateSettings", RequestID: string(memory.NewID()), Patch: asJSON(map[string]string{"city": "虚构城"})})
	finish()
	err = <-done
	failure, ok := err.(*worker.JobError)
	if !ok || failure.Code != "background_write_busy" || !failure.NoAttempt {
		t.Errorf("contention must defer without burning attempt: %v", err)
	}
	if snapErr != nil || commandErr != nil {
		t.Errorf("owner not released promptly: snapshot=%v command=%v", snapErr, commandErr)
	}
	if time.Since(start) > time.Second {
		t.Error("contended result held owner for over one second")
	}
}
