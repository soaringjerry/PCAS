package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestLibraryDeadlinesUseModelDatesAndRetainUncertainty(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	texts := []string{"Send the fictional report by September 12 at 10:00.", "虚构样稿在九月十二号上午十点交。", "虚构稿件的日期需要确认。", "虚构安排的日期早于说话时间。", "虚构展会在九月十二日，钟点没说。"}
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, memories := statusRegressionInput(t, r)
		items := []any{}
		for _, m := range memories {
			at := "2026-09-12T10:00:00+08:00"
			if m.Text == texts[2] {
				at = "not-a-date"
			}
			if m.Text == texts[3] {
				at = "2026-07-31T10:00:00+08:00"
			}
			if m.Text == texts[4] {
				at = "2026-09-12"
			}
			items = append(items, map[string]any{"n": m.N, "category": "event", "durable": false, "deadlines": []any{map[string]any{"kind": "deadline", "at": at, "recurrence": "", "title": "虚构交稿", "timeNote": "模型说明"}}})
		}
		secretaryModelReply(w, map[string]any{"items": items})
	})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	for _, text := range texts {
		ref := organizeTestMemory(t, s, scope, text)
		if _, err := s.pool.Exec(ctx, "UPDATE record_versions SET expressed_at='2026-08-01T09:00:00+08:00' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, ref.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ProcessOrganize(ctx, organizeTestJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}
	dates, err := s.ListDeadlines(ctx, scope, workspace.DeadlineQuery{})
	if err != nil || len(dates.Items) != 5 || calls.Load() != 1 {
		t.Fatal(dates, err, calls.Load())
	}
	due, _ := time.Parse(time.RFC3339, "2026-09-12T10:00:00+08:00")
	for _, d := range dates.Items {
		switch d.OriginalText {
		case texts[0], texts[1]:
			if d.Kind != "deadline" || d.At == nil || d.Expired != time.Now().After(due) {
				t.Fatal("model date discarded", d)
			}
		case texts[4]:
			if d.At == nil || *d.At != "2026-09-11T16:00:00Z" {
				t.Fatal("date-only value must use owner timezone", d)
			}
		case texts[2], texts[3]:
			if d.Kind != "unclear" || d.At != nil || d.DateStatus != "unclear" {
				t.Fatal("uncertainty must remain with provenance", d)
			}
		default:
			t.Fatal("lost original text", d)
		}
	}
	// The UI transition endpoint also retains all expired/unclear entries.
	response := b1HTTP(t, s, scope, "GET", "/v1/workspace/about", nil)
	var about workspace.About
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &about) != nil || len(about.Deadlines) != 5 {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestHandoverUsesIndependentRollingLimit(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	sections := []handoverSection{}
	for _, title := range handoverTitles {
		sections = append(sections, handoverSection{Title: title, Body: "虚构交接内容", Refs: []int{1}})
	}
	fake := b1Model(t, s, string(asJSON(handoverOutput{Sections: sections})))
	statusTestMemories(t, s, scope, 3)
	job := handoverTestJob(t, s, scope)
	seedStageUsage(t, s, scope, HandoverStage, 2)
	err := s.ProcessHandover(ctx, job)
	failure, ok := err.(*worker.JobError)
	if !ok || failure.Code != "memory.handover_hourly_limit" || !failure.NoAttempt || len(fake.all()) != 0 {
		t.Fatal("handover borrowed other stage capacity", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE background_usage SET created_at=now()-interval '61 minutes' WHERE id=(SELECT id FROM background_usage WHERE stage='memory.handover' ORDER BY id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	for stage, limit := range map[string]int{OrganizeStage: 40, CompareStage: 40, EntityCompareStage: 30, EntityCandidatesStage: 6} {
		seedStageUsage(t, s, scope, stage, limit)
	}
	if err := s.ProcessHandover(ctx, job); err != nil || len(fake.all()) != 1 {
		t.Fatal("other stages consumed handover slot", err, len(fake.all()))
	}
}
