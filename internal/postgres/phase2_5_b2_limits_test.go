package postgres_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B2_TwoHundredLatestAndRemainingEventuallyCompared(t *testing.T) {
	f := phase25B2NewFixtureTimeout(t, 3*time.Minute)
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
			if len(in.Memories) != 200 {
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
		want := i >= 5
		if first[fmt.Sprintf("虚构比较上限 %03d。", i)] != want {
			t.Errorf("first latest selection at i=%d", i)
		}
	}
	var marked int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND compared=1`, f.scope.OwnerID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 205 {
		t.Errorf("remaining comparison markers=%d want 205", marked)
	}
	if calls < 2 {
		t.Error("remaining inputs never compared")
	}
	f.assertRevisions(t, refs...)
}

// R2-6 and batch 1 R12 share one allowance across all three model purposes.
func TestPhase25B2_OrganizeCompareAndEntityShareOneHundredTwentyCallsPerHour(t *testing.T) {
	const hourlyLimit, initialOrganizeCalls = 120, 20
	f := phase25B2NewFixtureTimeout(t, 8*time.Minute)
	var refs []memory.Ref
	var organizeCalls, groupCalls, entityCalls atomic.Int32
	model := f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		entities, err := phase25B2Entities(request)
		if err != nil {
			t.Error(err)
			return phase25B234ModelReply{status: 400}
		}
		if len(entities) > 0 {
			entityCalls.Add(1)
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		prompt, err := phase25B3Prompt(request)
		if err != nil {
			t.Error(err)
			return phase25B234ModelReply{status: 400}
		}
		if strings.Contains(prompt, `"protected"`) {
			groupCalls.Add(1)
			return phase25B2JSON(phase25B2Empty())
		}
		organizeCalls.Add(1)
		return phase25B234ModelReply{content: `{"items":[{"n":1,"category":"progress","durable":true}],"new":[]}`}
	})
	// Isolate the fixture queue while retaining actual memories and usage rows.
	scheduleOrganize := func() {
		t.Helper()
		f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.organize:%'`, f.scope.OwnerID)
		if _, err := f.store.ScheduleOrganize(f.ctx, time.Now().Add(11*time.Minute)); err != nil {
			t.Fatal(err)
		}
		f.exec(t, `UPDATE memory_jobs SET available_at=least(available_at,now()) WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID)
	}
	organizeJob := func() {
		t.Helper()
		scheduleOrganize()
		job, err := f.store.Claim(f.ctx, time.Minute)
		if err != nil || job == nil {
			t.Fatalf("organize claim=%+v error=%v", job, err)
		}
		if !strings.HasPrefix(job.Stage, "memory.organize:") {
			t.Fatalf("unexpected organize stage=%s", job.Stage)
		}
		if err := f.store.ProcessOrganize(f.ctx, *job); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < initialOrganizeCalls; i++ {
		refs = append(refs, f.claim(t, fmt.Sprintf("虚构共享额度整理便签%03d。", i)))
		organizeJob()
	}
	if got := f.usage(t, "organize"); got != initialOrganizeCalls {
		t.Fatalf("initial organize ledger=%d want %d", got, initialOrganizeCalls)
	}
	for i := 0; i < hourlyLimit+1; i++ {
		g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", fmt.Sprintf("虚构配额组%03d", i))), Type: "topic"}
		// Separate subjects avoid a legitimate aggregate comparison marking all
		// topic inputs at once. Every group remains a real eligibility candidate.
		f.subject = f.entity(t, "person", fmt.Sprintf("FictitiousQuotaPerson%03d", i))
		r := f.claim(t, fmt.Sprintf("虚构配额便签%03d。", i))
		f.labels(t, r, "progress", true, 1, g)
		refs = append(refs, r)
	}
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构配额合用项目")), Type: "project"}
	for i := 0; i < 2; i++ {
		id := f.entity(t, "person", "FictitiousSameQuotaPerson")
		refs = append(refs, f.personTexts(t, g, id, fmt.Sprintf("虚构配额实体证据 %d。", i))...)
	}
	for i := 0; i < hourlyLimit-initialOrganizeCalls; i++ {
		f.scheduleCompare(t)
		before := len(model.calls())
		if f.compareJob(t) == "" {
			t.Fatalf("comparison queue stopped at combined call %d", before)
		}
		if got := len(model.calls()); got != before+1 {
			t.Fatalf("completed comparison model calls=%d want %d", got, before+1)
		}
	}
	if organizeCalls.Load() == 0 || groupCalls.Load() == 0 || entityCalls.Load() == 0 {
		t.Fatalf("all three categories required: organize=%d compare=%d entity=%d", organizeCalls.Load(), groupCalls.Load(), entityCalls.Load())
	}
	assertBoundary := func() {
		t.Helper()
		if got := len(model.calls()); got != hourlyLimit {
			t.Errorf("shared hourly calls=%d want %d", got, hourlyLimit)
		}
		if got := f.usage(t, "organize") + f.usage(t, "compare"); got != hourlyLimit {
			t.Errorf("shared actual usage=%d want %d", got, hourlyLimit)
		}
	}
	assertBoundary()
	// Execute through the actual worker so normal quota deferral is honored.
	// Neither remaining comparison work nor a new organize batch gets call 121.
	deferAtBoundary := func(prefix string, handler worker.Handler) {
		t.Helper()
		var stage string
		if err := f.db.QueryRow(f.ctx, `SELECT coalesce((SELECT stage FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage LIKE $2 ORDER BY priority,created_at LIMIT 1),'')`, f.scope.OwnerID, prefix+"%").Scan(&stage); err != nil {
			t.Fatal(err)
		}
		if stage != "" {
			f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND state='queued' AND stage=$2`, f.scope.OwnerID, stage)
			runner := worker.New(f.store, map[string]worker.Handler{stage: handler, "memory.compare": f.store.ProcessCompare, "memory.entity_compare": f.store.ProcessEntityCompare, "memory.organize": f.store.ProcessOrganize}, slog.Default())
			if _, err := runner.RunOnce(f.ctx); err != nil {
				t.Fatal(err)
			}
		}
		assertBoundary()
	}
	f.scheduleCompare(t)
	deferAtBoundary("memory.compare:", f.store.ProcessCompare)
	// A fresh same-name pair remains eligible even if the earlier pair was
	// marked different. Exercise the entity handler at the shared boundary too.
	entityGroup := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构边界实体项目")), Type: "project"}
	for i := 0; i < 2; i++ {
		id := f.entity(t, "person", "FictitiousBoundaryQuotaPerson")
		refs = append(refs, f.personTexts(t, entityGroup, id, fmt.Sprintf("虚构边界实体证据 %d。", i))...)
	}
	f.scheduleCompare(t)
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.compare:%'`, f.scope.OwnerID)
	deferAtBoundary("memory.entity_compare:", f.store.ProcessEntityCompare)
	last := f.claim(t, "虚构共享额度第一百二十一调用哨兵。")
	refs = append(refs, last)
	scheduleOrganize()
	deferAtBoundary("memory.organize:", f.store.ProcessOrganize)
	var organized, attempts int
	if err := f.db.QueryRow(f.ctx, `SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, last.ID).Scan(&organized, &attempts); err != nil {
		t.Fatal(err)
	}
	if organized != 0 || attempts != 0 {
		t.Errorf("quota-deferred sentinel organized=%d attempts=%d", organized, attempts)
	}
	// Preserve real ledger history and advance only this test owner's hour.
	for _, tc := range []struct{ table, column string }{{"model_usage", "at"}, {"background_usage", "created_at"}, {"memory_jobs", "available_at"}} {
		f.exec(t, "UPDATE "+tc.table+" SET "+tc.column+"="+tc.column+"-interval '1 hour 1 second' WHERE owner_id=$1", f.scope.OwnerID)
	}
	f.scheduleCompare(t)
	if f.compareJob(t) == "" {
		t.Fatal("next-hour comparison not resumed")
	}
	organizeJob()
	if got := len(model.calls()); got != hourlyLimit+2 {
		t.Errorf("next-hour calls=%d want %d", got, hourlyLimit+2)
	}
	f.assertRevisions(t, refs...)
}
