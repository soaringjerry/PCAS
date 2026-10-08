package postgres

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase35T2RandomReverseUndo(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	for _, seed := range []int64{3501, 3502, 3503} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			scope := owner()
			phase35Snapshot(t, s, scope)
			baseline, _ := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构起始事项"})
			initial := phase35Business(baseline)
			actions := []string{}
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < 24; i++ {
				st := phase35Snapshot(t, s, scope)
				if rng.Intn(3) != 0 {
					kind := "task"
					if rng.Intn(2) == 0 {
						kind = "idea"
					}
					it := phase35Direct(fmt.Sprintf("虚构随机种子%d原话%02d", seed, i), kind)
					ref := phase35Source(t, s, scope, []extractedItem{it})
					phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
						secretaryModelReply(w, extracted{Items: []extractedItem{it}})
					})
					phase35Extract(t, s, scope, ref)
					after := phase35Snapshot(t, s, scope)
					found := false
					for _, work := range phase35Work(after) {
						if work.Title == it.Text {
							actions = append(actions, phase35RequireCreation(t, s, scope, work, ref))
							found = true
						}
					}
					if !found {
						t.Fatal("random automatic creation missing")
					}
				} else {
					items := phase35Work(st)
					it := items[rng.Intn(len(items))]
					_, id := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: it.ID, Title: fmt.Sprintf("虚构编辑%d-%d", seed, i)})
					actions = append(actions, id)
				}
			}
			for i := len(actions) - 1; i >= 0; i-- {
				if _, err := s.Undo(ctx, scope, actions[i]); err != nil {
					t.Fatalf("seed=%d inverse index=%d: %v", seed, i, err)
				}
			}
			final := phase35Business(phase35Snapshot(t, s, scope))
			if !reflect.DeepEqual(initial, final) {
				t.Fatalf("seed=%d reverse undo differs before=%v after=%v", seed, initial, final)
			}
			t.Logf("seed=%d operations=%d fully reversed; business state equal (audit/version counters excluded)", seed, len(actions))
		})
	}
}

// Audit/history/version counters intentionally record undo. Every mutable work
// field except audit metadata is compared, including provenance and sources.
func phase35Business(st workspace.State) map[string]workspace.Item {
	out := map[string]workspace.Item{}
	for _, it := range append(phase35Work(st), st.Projects...) {
		it.Version = 0
		it.UpdatedAt = ""
		it.History = nil
		it.Evolution = nil
		speech := []workspace.SourceRef{}
		for _, source := range it.Sources {
			if source.Excerpt != "" {
				speech = append(speech, source)
			}
		}
		it.Sources = speech
		out[it.ID] = it
	}
	return out
}
func TestPhase35T2ConcurrentExtractionSchedulerAndUserHTTP(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	it := phase35Direct("虚构并发只建一条待办", "task")
	ref := phase35Source(t, s, scope, []extractedItem{it})
	a := leaseStage(t, s, scope, ref, "source.extract")
	b := leaseStage(t, s, scope, ref, "source.extract:0")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			secretaryModelReply(w, extracted{Items: []extractedItem{it}})
		case <-r.Context().Done():
		}
	})
	result := make(chan error, 2)
	go func() { result <- s.ProcessExtraction(ctx, a) }()
	go func() { result <- s.ProcessExtraction(ctx, b) }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case err := <-result:
			t.Fatalf("processor returned before provider overlap: %v", err)
		case <-time.After(15 * time.Second):
			t.Fatal("no provider overlap")
		}
	}
	h := phase35HTTP(t, s, scope)
	front := make(chan error, 2)
	go func() { _, err := s.ScheduleOrganize(ctx, time.Now()); front <- err }()
	go func() {
		code, raw, err := h.call(ctx, "POST", "/v1/workspace/commands", map[string]any{"type": "addIdea", "title": "虚构并发用户事项", "requestId": string(memory.NewID())})
		if err == nil && code != 200 {
			err = fmt.Errorf("HTTP %d %s", code, raw)
		}
		front <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-front:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("foreground/scheduler blocked during provider")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("processor did not finish")
		}
	}
	st := phase35Snapshot(t, s, scope)
	if len(st.Tasks) != 1 || len(st.Ideas) != 1 || calls.Load() != 2 {
		t.Fatalf("concurrent state/tasks=%d ideas=%d provider_attempts=%d", len(st.Tasks), len(st.Ideas), calls.Load())
	}
	phase35RequireCreation(t, s, scope, st.Tasks[0], ref)
}
func TestPhase35T2DeletedEvidenceCannotRecreateWork(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	it := phase35Direct("虚构删除原话后的自动事项", "task")
	ref := phase35Source(t, s, scope, []extractedItem{it})
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, extracted{Items: []extractedItem{it}})
	})
	phase35Extract(t, s, scope, ref)
	st := phase35Snapshot(t, s, scope)
	if len(st.Tasks) != 1 {
		t.Fatal("missing precondition task")
	}
	action := phase35Action(t, s, scope, st.Tasks[0].ID)
	err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Undo(ctx, scope, action)
	if err != nil && !errors.Is(err, memory.ErrNotFound) && !errors.Is(err, workspace.ErrChangedSince) && !errors.Is(err, workspace.ErrNewerAction) && !errors.Is(err, workspace.ErrExpired) {
		t.Fatal("deletion undo must safely refuse or remove stale work", err)
	}
	source, readErr := s.GetSource(ctx, scope, ref.ID, 1)
	if readErr == nil && source.Source.State == "active" {
		t.Fatal("undo resurrected deleted speech")
	}
}
