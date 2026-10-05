package postgres_test

import (
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"strings"
	"testing"
	"time"
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
func TestPhase25B2_CompareAndEntityShareThirtyCallsPerHour(t *testing.T) {
	f := phase25B2NewFixtureTimeout(t, 3*time.Minute)
	for i := 0; i < 31; i++ {
		g := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", fmt.Sprintf("虚构配额组%02d", i))), Type: "topic"}
		// Each note has a separate fictitious subject; otherwise an aggregate
		// person group can legitimately mark all 31 topics in one comparison.
		f.subject = f.entity(t, "person", fmt.Sprintf("FictitiousQuotaPerson%02d", i))
		r := f.claim(t, fmt.Sprintf("虚构配额便签%02d。", i))
		f.labels(t, r, "progress", true, 1, g)
	}
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构配额合用项目")), Type: "project"}
	for i := 0; i < 2; i++ {
		id := f.entity(t, "person", "FictitiousSameQuotaPerson")
		f.personTexts(t, g, id, fmt.Sprintf("虚构配额实体证据 %d。", i))
	}
	groupCalls, entityCalls := 0, 0
	model := f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		entities, e := phase25B2Entities(r)
		if e != nil {
			t.Error(e)
			return phase25B234ModelReply{status: 400}
		}
		if len(entities) > 0 {
			entityCalls++
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		groupCalls++
		return phase25B2JSON(phase25B2Empty())
	})
	f.scheduleCompare(t)
	for i := 0; i < 30; i++ {
		if f.compareJob(t) == "" {
			f.scheduleCompare(t)
			if f.compareJob(t) == "" {
				rows, e := f.db.Query(f.ctx, `SELECT stage,state,error_code,available_at FROM memory_jobs WHERE owner_id=$1 ORDER BY created_at`, f.scope.OwnerID)
				if e != nil {
					t.Fatal(e)
				}
				for rows.Next() {
					var stage, state, code string
					var at time.Time
					_ = rows.Scan(&stage, &state, &code, &at)
					t.Logf("quota queue %s %s %s %s", stage, state, code, at)
				}
				rows.Close()
				t.Fatalf("quota stopped before 30 at %d actual=%d usage=%d", i, len(model.calls()), f.usage(t, "compare"))
			}
		}
		f.scheduleCompare(t)
	}
	// Check the actual boundary without turning expected quota deferral into an
	// assertion failure. The model must not be called for the 31st group.
	f.exec(t, `UPDATE memory_jobs SET available_at=least(available_at,now()) WHERE owner_id=$1 AND state='queued' AND stage LIKE 'memory.compare:%'`, f.scope.OwnerID)
	j, err := f.store.Claim(f.ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if j != nil {
		if strings.HasPrefix(j.Stage, "memory.entity_compare:") {
			err = f.store.ProcessEntityCompare(f.ctx, *j)
		} else {
			err = f.store.ProcessCompare(f.ctx, *j)
		}
		if err == nil {
			t.Error("31st processing unexpectedly succeeded")
		}
	}
	if groupCalls == 0 || entityCalls == 0 {
		t.Errorf("both categories required: compare=%d entity=%d", groupCalls, entityCalls)
	}
	if len(model.calls()) != 30 || f.usage(t, "compare") != 30 {
		t.Errorf("actual hour calls=%d ledger=%d", len(model.calls()), f.usage(t, "compare"))
	}
}
