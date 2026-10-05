package postgres_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestPhase25B3_ConcurrentCorrectionOrDeletionDuringCardModel(t *testing.T) {
	for _, operation := range []string{"correct", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g, refs := f.cardGroup(t, 5)
			entered, release := make(chan struct{}), make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			f.model(t, func(r *http.Request, call int, request phase25B234ModelRequest) phase25B234ModelReply {
				input, err := phase25B3CardInput(request)
				if err != nil {
					text, e := phase25B3Prompt(request)
					if e != nil || !strings.Contains(text, `"cards"`) {
						return phase25B234ModelReply{status: 400}
					}
					out, e := phase25B3HandoverJSON(nil, nil)
					if e != nil {
						return phase25B234ModelReply{status: 500}
					}
					return phase25B234ModelReply{content: out}
				}
				if call == 1 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return phase25B234ModelReply{status: 503}
					}
				}
				return phase25B3JSON(phase25B3All(input))
			})
			f.scheduleStatus(t, time.Now().Add(11*time.Minute))
			f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.card:%' AND state='queued'`, f.scope.OwnerID)
			j, err := f.store.Claim(f.ctx, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if j == nil || !strings.HasPrefix(j.Stage, "memory.card:") {
				t.Fatalf("no real card job leased: %+v", j)
			}
			done := make(chan error, 1)
			go func() { done <- f.store.ProcessCard(f.ctx, *j) }()
			select {
			case <-entered:
			case <-f.ctx.Done():
				t.Fatal("model barrier not reached")
			}
			old := refs[0]
			if operation == "correct" {
				refs[0] = f.correct(t, old, "虚构模型期间纠正：周五交。")
			} else {
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{old}}); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			released = true
			select {
			case err := <-done:
				if err != nil {
					var changed *worker.JobError
					if errors.As(err, &changed) && changed.Code == "card_changed" && changed.Retry && !changed.Until.IsZero() {
						if err := f.store.Defer(f.ctx, *j, changed.Code, changed.Until, changed.NoAttempt); err != nil {
							t.Fatal(err)
						}
					} else if err.Error() == "card_changed" {
						if err := f.store.Retry(f.ctx, *j, "card_changed"); err != nil {
							t.Fatal(err)
						}
					} else {
						t.Fatal(err)
					}
				}
			case <-f.ctx.Done():
				t.Fatal("card job did not finish")
			}
			var invalid int
			if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM status_card_items WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$3`, f.scope.OwnerID, old.ID, old.Version).Scan(&invalid); err != nil {
				t.Fatal(err)
			}
			if invalid != 0 {
				t.Errorf("writer persisted %d invalid snapshot items", invalid)
			}
			f.buildStatus(t, time.Now().Add(11*time.Minute))
			want := refs
			if operation == "delete" {
				want = refs[1:]
			}
			phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, "entity:"+g.EntityID)), want...)
			if operation == "correct" {
				f.assertRevisions(t, refs...)
			} else {
				f.assertRevisions(t, refs[1:]...)
			}
		})
	}
}
