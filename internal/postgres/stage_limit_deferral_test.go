package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestStageLimitDeferralMovesWaitingJobsOfThatStageWithOneEvent(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	anchor := mustIngest(t, s, scope, input()).Ref
	if _, err := s.pool.Exec(ctx, `DELETE FROM memory_jobs WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	enqueue := func(stage string, available time.Time) string {
		id := string(memory.NewID())
		if _, err := s.pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at) VALUES($1,$2,$3,$4,$5,0,$6)`, id, string(scope.OwnerID), string(anchor.ID), anchor.Version, stage, available); err != nil {
			t.Fatal(err)
		}
		return id
	}
	past, window, later := time.Now().Add(-time.Minute), time.Now().Add(20*time.Minute).Truncate(time.Second), time.Now().Add(3*time.Hour).Truncate(time.Second)
	first := enqueue(TopicProjectStage+":a:1", past.Add(-time.Minute))
	waiting := []string{enqueue(TopicProjectStage+":b:1", past), enqueue(TopicProjectStage+":c:1", past)}
	scheduledLater := enqueue(TopicProjectStage+":d:1", later)
	otherStage := enqueue(DateTidyStage+":e:1", past)

	job, err := s.Claim(ctx, time.Minute)
	if err != nil || job == nil || string(job.ID) != first {
		t.Fatal(job, err)
	}
	if err := s.Defer(ctx, *job, "topic_project_hourly_limit", window, true); err != nil {
		t.Fatal(err)
	}
	at := func(id string) time.Time {
		var v time.Time
		if err := s.pool.QueryRow(ctx, `SELECT available_at FROM memory_jobs WHERE id=$1 AND state='queued'`, id).Scan(&v); err != nil {
			t.Fatal(id, err)
		}
		return v
	}
	for _, id := range append(waiting, first) {
		if !at(id).Equal(window) {
			t.Fatal("a waiting job of the limited stage can still be taken before the window", at(id), window)
		}
	}
	if !at(scheduledLater).Equal(later) || !at(otherStage).Before(time.Now()) {
		t.Fatal("a later job or another stage was moved", at(scheduledLater), at(otherStage))
	}
	var deferrals int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_stage_events WHERE owner_id=$1 AND outcome='deferred'`, string(scope.OwnerID)).Scan(&deferrals); err != nil || deferrals != 1 {
		t.Fatal("the limit was recorded more than once", deferrals, err)
	}
	// Another reason keeps the earlier rule: only the deferred job moves.
	next, err := s.Claim(ctx, time.Minute)
	if err != nil || next == nil || string(next.ID) != otherStage {
		t.Fatal(next, err)
	}
}
