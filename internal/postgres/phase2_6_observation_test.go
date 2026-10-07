package postgres

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPhase26A2SchedulerReportsConcreteLockFailure(t *testing.T) {
	phase26Finding(t, "S-P26-003")
	f := phase26LoadFixture(t)
	phase26NewModel(t, f)
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, f.Scope.OwnerID)
	lock, err := f.Store.pool.Begin(f.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(f.Context, `SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE`, f.Scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithTimeout(f.Context, 15*time.Second)
	defer stop()
	done := make(chan struct{})
	go func() { defer close(done); f.Store.RunCompare(runCtx, slog.Default()) }()
	reason := ""
	for reason == "" && runCtx.Err() == nil {
		if err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce((SELECT reason FROM background_stage_events WHERE stage='memory.compare' AND outcome='failure' ORDER BY id DESC LIMIT 1),'')`).Scan(&reason); err != nil {
			t.Fatal(err)
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-runCtx.Done():
		}
	}
	stop()
	if err := lock.Rollback(f.Context); err != nil {
		t.Fatal(err)
	}
	<-done
	if reason == "" {
		t.Fatal("actual scheduler runner did not record its lock failure")
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual scheduler runner reason=%s observability=%s", reason, health)
	if !strings.Contains(string(health), "background_write_busy") || strings.Contains(string(health), "schedule_*worker.JobError") {
		t.Error("schedule failure reason hides the actual lock contention cause")
	}
}

func TestPhase26A2LongDeferralWarnsOnceAndResetsAfterCompletion(t *testing.T) {
	f := phase26LoadFixture(t)
	phase26NewModel(t, f)
	phase26Isolate(t, f, OrganizeStage)
	if _, err := f.Store.ScheduleOrganize(f.Context, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, OrganizeStage)
	j := phase26ClaimStage(t, f, OrganizeStage)
	if err := f.Store.Defer(f.Context, j, "fictitious_deferral", time.Now().Add(time.Minute), true); err != nil {
		t.Fatal(err)
	}
	phase26Exec(t, f, `UPDATE background_job_deferrals SET started_at=now()-interval '31 minutes' WHERE job_id=$1`, j.ID)
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	defer slog.SetDefault(old)
	for i := 0; i < 2; i++ {
		if err := f.Store.warnLongDeferrals(f.Context); err != nil {
			t.Fatal(err)
		}
	}
	var entries []map[string]any
	scanner := bufio.NewScanner(&log)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if len(entries) != 1 || entries[0]["level"] != "WARN" || entries[0]["job_id"] != string(j.ID) {
		t.Fatalf("long-deferral alert must identify this job once: %s", log.String())
	}
	phase26Exec(t, f, `UPDATE memory_jobs SET available_at=now() WHERE id=$1`, j.ID)
	retry := phase26ClaimStage(t, f, OrganizeStage)
	if err := f.Store.ProcessOrganize(f.Context, retry); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM background_job_deferrals WHERE job_id=$1`, j.ID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Errorf("completed task retains continuous deferral alert state: %d", retained)
	}
	t.Logf("31-minute synthetic retained clock; one actual WARN, no duplicate, completion resets alert")
}
