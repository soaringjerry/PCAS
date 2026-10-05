package postgres_test

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B1_R12_ThirtyBatchesPerHour(t *testing.T) {
	f := phase25B1NewFixture(t)
	fake := f.model(t, phase25B1ModelJSON(t, phase25B1Items(40, "event", false)))
	for i := 0; i < 30; i++ {
		f.claim(t, fmt.Sprintf("虚构小时限额便签%d。", i))
		f.batch(t)
	}
	last := f.claim(t, "虚构第三十一批的便签。")
	queued := f.schedule(t)
	if queued == 1 {
		var stage string
		if err := f.db.QueryRow(f.ctx, `SELECT stage FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		runner := worker.New(f.store, map[string]worker.Handler{stage: f.store.ProcessOrganize, "memory.organize": f.store.ProcessOrganize}, slog.Default())
		if _, err := runner.RunOnce(f.ctx); err != nil {
			t.Fatal(err)
		}
	} else if queued != 0 {
		t.Fatalf("queued %d tasks", queued)
	}
	var reservations int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM background_usage WHERE owner_id=$1 AND created_at>now()-interval '1 hour'`, f.scope.OwnerID).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	t.Logf("after thirty requests: calls=%d recent reservations=%d", fake.calls(), reservations)
	f.checkClaim(t, last, "unknown", 0, 0)
	if fake.calls() != 30 {
		t.Errorf("hourly model calls = %d", fake.calls())
	}
	// Age only the owned fixture's reservations to represent the next hour.
	f.exec(t, `UPDATE background_usage SET created_at=now()-interval '61 minutes' WHERE owner_id=$1`, f.scope.OwnerID)
	f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID)
	f.batch(t)
	f.checkClaim(t, last, "event", 1, 0)
	if fake.calls() != 31 {
		t.Errorf("next-hour calls = %d", fake.calls())
	}
}

func TestPhase25B1_R13_DailyBudgetDefersWithoutAttempts(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构每日额度便签。")
	fake := f.model(t, phase25B1ModelJSON(t, phase25B1Items(1, "event", false)))
	fake.registry.Config.Providers[0].CostMode = ""
	fake.registry.Config.Providers[0].InputPerMillion = 1
	fake.registry.Config.Providers[0].OutputPerMillion = 1
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE workspace_owners SET settings=jsonb_set(jsonb_set(settings,'{dailyBudget}','0.000001'),'{timezone}','"UTC"') WHERE owner_id=$1`, f.scope.OwnerID)
	queued := f.schedule(t)
	if queued == 1 {
		var stage string
		if err := f.db.QueryRow(f.ctx, `SELECT stage FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		runner := worker.New(f.store, map[string]worker.Handler{stage: f.store.ProcessOrganize, "memory.organize": f.store.ProcessOrganize}, slog.Default())
		worked, err := runner.RunOnce(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			t.Fatal("quota task not claimed")
		}
		var available time.Time
		if err := f.db.QueryRow(f.ctx, `SELECT available_at FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID).Scan(&available); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		nextDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		if available.Before(nextDay) || !available.Before(nextDay.Add(24*time.Hour)) {
			t.Errorf("budget deferral = %s, expected next cycle %s", available, nextDay)
		}
	} else if queued != 0 {
		t.Fatalf("quota queued %d tasks", queued)
	}
	f.checkClaim(t, ref, "unknown", 0, 0)
	if fake.calls() != 0 {
		t.Error("model called despite insufficient daily quota")
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize.Done != 0 || state.Organize.Total != 1 {
		t.Errorf("quota progress = %+v", state.Organize)
	}
	// More quota makes the same pending work eligible without losing progress.
	f.exec(t, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','10') WHERE owner_id=$1`, f.scope.OwnerID)
	f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID)
	f.batch(t)
	f.checkClaim(t, ref, "event", 1, 0)
	if fake.calls() != 1 {
		t.Errorf("calls after raising budget = %d", fake.calls())
	}
}
