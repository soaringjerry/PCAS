package postgres_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B2_AllStableBlocksEventuallyCompared(t *testing.T) {
	f := phase25B2NewFixtureTimeout(t, 3*time.Minute)
	// A place subject is outside the canonical person/project/topic/area groups;
	// this fixture therefore measures one topic's complete six-batch coverage.
	f.subject = f.entity(t, "place", "虚构比较场地")
	f.sharedSubject = true
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构比较上限")), Type: "topic"}
	var refs []memory.Ref
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 205; i++ {
		r := f.claim(t, fmt.Sprintf("虚构比较上限 %03d。", i))
		f.labels(t, r, "progress", true, 1, g)
		f.exec(t, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2`, f.scope.OwnerID, r.ID, base.Add(time.Duration(i)*time.Minute))
		refs = append(refs, r)
	}
	calls := 0
	first := map[string]bool{}
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		calls++
		if len(in.Memories) > 200 {
			t.Errorf("input cap=%d", len(in.Memories))
		}
		if calls == 1 {
			if len(in.Memories) != 100 {
				t.Errorf("first input=%d", len(in.Memories))
			}
			for _, m := range in.Memories {
				first[m.Text] = true
				if m.ExpressedAt == "" {
					t.Error("expressedAt not provided")
				}
			}
		}
		return phase25B2Empty()
	})
	f.runCompare(t)
	for i := 0; i < 205; i++ {
		want := i < 100
		if first[fmt.Sprintf("虚构比较上限 %03d。", i)] != want {
			t.Errorf("first latest selection at i=%d", i)
		}
	}
	var marked int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND compared=$2`, f.scope.OwnerID, postgres.CompareVersion).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 205 {
		t.Errorf("remaining comparison markers=%d want 205", marked)
	}
	if calls != 6 {
		t.Errorf("remaining inputs never compared: calls=%d want 6", calls)
	}
	f.assertRevisions(t, refs...)
}

// Independent rolling limits preserve completed history; pending queue isolation
// removes queued jobs only. This replaces the obsolete skipped shared-cap case.
func TestPhase25B2_IndependentHourlyLimitsRetainHistory(t *testing.T) {
	f := phase25B2NewFixtureTimeout(t, 5*time.Minute)
	model := f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		prompt, err := phase25B3Prompt(request)
		if err != nil {
			t.Error(err)
			return phase25B234ModelReply{status: 400}
		}
		if strings.Contains(prompt, `"protected"`) {
			return phase25B2JSON(phase25B2Empty())
		}
		return phase25B234ModelReply{content: `{"items":[{"n":1,"category":"progress","durable":true,"deadlines":[]}]}`}
	})
	var refs []memory.Ref
	for i := 0; i < 40; i++ {
		refs = append(refs, f.claim(t, fmt.Sprintf("虚构独立额度整理便签%03d", i)))
		f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE 'memory.organize:%'`, f.scope.OwnerID)
		if _, err := f.store.ScheduleOrganize(f.ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		j, err := f.store.Claim(f.ctx, time.Minute)
		if err != nil || j == nil {
			t.Fatal(j, err)
		}
		if err := f.store.ProcessOrganize(f.ctx, *j); err != nil {
			t.Fatal(err)
		}
	}
	// Classification is full, but comparison still has its own 40 slots.
	f.groupTexts(t, "虚构独立比较甲", "虚构独立比较乙")
	f.scheduleCompare(t)
	if f.compareJob(t) == "" {
		t.Fatal("classification saturation blocked comparison")
	}
	if len(model.calls()) != 41 {
		t.Fatal("unexpected invocation count", len(model.calls()))
	}
	last := f.claim(t, "虚构分类额度哨兵")
	refs = append(refs, last)
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE 'memory.organize:%'`, f.scope.OwnerID)
	if _, err := f.store.ScheduleOrganize(f.ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	j, err := f.store.Claim(f.ctx, time.Minute)
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	err = f.store.ProcessOrganize(f.ctx, *j)
	var quota *worker.JobError
	if !errors.As(err, &quota) || quota.Code != "organize_hourly_limit" || !quota.NoAttempt {
		t.Fatal("classification borrowed comparison quota", err)
	}
	var orphaned int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM background_usage b LEFT JOIN memory_jobs j ON j.id=b.job_id WHERE b.owner_id=$1 AND j.id IS NULL`, f.scope.OwnerID).Scan(&orphaned); err != nil || orphaned != 0 {
		t.Fatal("queue cleanup removed completed history", orphaned, err)
	}
	f.exec(t, `UPDATE background_usage SET created_at=now()-interval '61 minutes' WHERE owner_id=$1 AND stage='memory.organize'`, f.scope.OwnerID)
	if err := f.store.ProcessOrganize(f.ctx, *j); err != nil {
		t.Fatal("released classification slot blocked", err)
	}
	if len(model.calls()) != 42 {
		t.Fatal(len(model.calls()))
	}
	f.assertRevisions(t, refs...)
}
