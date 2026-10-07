package postgres_test

import (
	"errors"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhase25B2_CompareDoesNotCallModelDuringOrganize(t *testing.T) {
	f := phase25B2NewFixture(t)
	_, refs := f.groupTexts(t, "虚构整理比较锁甲。", "虚构整理比较锁乙。")
	extra := f.claim(t, "虚构尚未整理的锁哨兵。")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var active atomic.Int32
	var organizeCalls, compareCalls atomic.Int32
	f.model(t, func(r *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		if active.Add(1) != 1 {
			t.Error("organize and compare model calls overlapped")
		}
		defer active.Add(-1)
		text, e := phase25B3Prompt(request)
		if e != nil {
			t.Error(e)
			return phase25B234ModelReply{status: 400}
		}
		if strings.Contains(text, `"protected"`) {
			compareCalls.Add(1)
			return phase25B2JSON(phase25B2Empty())
		}
		organizeCalls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return phase25B234ModelReply{status: 503}
		}
		return phase25B234ModelReply{content: `{
 "items": [
  {
   "category": "progress",
   "deadlines": [],
   "durable": true,
   "n": 1
  }
 ],
 "new": []
}`}
	})
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1`, f.scope.OwnerID)
	if n, e := f.store.ScheduleOrganize(f.ctx, time.Now().Add(11*time.Minute)); e != nil || n == 0 {
		t.Fatalf("organize queued=%d err=%v", n, e)
	}
	f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.organize:%'`, f.scope.OwnerID)
	org, e := f.store.Claim(f.ctx, time.Minute)
	if e != nil || org == nil {
		t.Fatalf("organize claim=%+v err=%v", org, e)
	}
	if !strings.HasPrefix(org.Stage, "memory.organize:") {
		t.Fatalf("wrong organize stage=%s", org.Stage)
	}
	orgDone := make(chan error, 1)
	go func() { orgDone <- f.store.ProcessOrganize(f.ctx, *org) }()
	select {
	case <-entered:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	scheduled := make(chan error, 1)
	go func() { _, e := f.store.ScheduleCompare(f.ctx, time.Now().Add(11*time.Minute)); scheduled <- e }()
	scheduledJoined := false
	select {
	case e := <-scheduled:
		scheduledJoined = true
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(200 * time.Millisecond):
	}
	// Any comparison job admitted during the active organize call must wait.
	f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.compare:%' AND state='queued'`, f.scope.OwnerID)
	cmp, e := f.store.Claim(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	var cmpDone chan error
	if cmp != nil {
		if !strings.HasPrefix(cmp.Stage, "memory.compare:") {
			t.Fatalf("unexpected parallel job=%s", cmp.Stage)
		}
		cmpDone = make(chan error, 1)
		go func() { cmpDone <- f.store.ProcessCompare(f.ctx, *cmp) }()
	}
	select {
	case <-time.After(200 * time.Millisecond):
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	if compareCalls.Load() != 0 {
		t.Error("compare model admitted during organize")
	}
	once.Do(func() { close(release) })
	select {
	case e := <-orgDone:
		if e != nil {
			t.Fatal(e)
		}
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	if cmpDone != nil {
		select {
		case cmpErr := <-cmpDone:
			if cmpErr != nil {
				t.Logf("comparison deferred during organize: %v", cmpErr)
				var deferred *worker.JobError
				if errors.As(cmpErr, &deferred) && deferred.Retry && !deferred.Until.IsZero() {
					if e := f.store.Defer(f.ctx, *cmp, deferred.Code, deferred.Until, deferred.NoAttempt); e != nil {
						t.Fatal(e)
					}
				} else {
					if e := f.store.Retry(f.ctx, *cmp, cmpErr.Error()); e != nil {
						t.Fatal(e)
					}
				}
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	if !scheduledJoined {
		select {
		case e := <-scheduled:
			if e != nil {
				t.Fatal(e)
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	// Advance the disposable inputs and real ledgers through the debounce/retry
	// boundary without deleting quota records or manufacturing a new Job.
	f.advanceStatusTime(t, 11*time.Minute)
	// After the lock is free, actual comparison must become possible.
	if compareCalls.Load() == 0 {
		f.runCompare(t)
	}
	if organizeCalls.Load() != 1 || compareCalls.Load() == 0 {
		t.Errorf("actual calls organize=%d compare=%d", organizeCalls.Load(), compareCalls.Load())
	}
	f.assertRevisions(t, append(refs, extra)...)
	var p workspace.MemoryPage
	f.get(t, "/v1/workspace/memories", &p)
}
