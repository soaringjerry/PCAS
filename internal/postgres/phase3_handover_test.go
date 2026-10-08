package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func phase3OnlyProject(t *testing.T, f *phase3LoadedFixture, id string) {
	t.Helper()
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE work_items SET status='done',document=jsonb_set(document,'{status}','"done"') WHERE owner_id=$1 AND kind='project' AND id<>$2`, f.Scope.OwnerID, id)
	// Jobs the global schedule already queued for the other projects would be
	// claimed first and acknowledged as done; only this project's work stays.
	phase26Exec(t, f.phase26LoadedFixture, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.project_handover:%' AND stage NOT LIKE 'memory.project_handover:'||$2||':%'`, f.Scope.OwnerID, id)
}
func phase3AgeHandoverClock(t *testing.T, f *phase3LoadedFixture) {
	t.Helper()
	rows, err := f.Store.pool.Query(f.Context, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public' AND table_name LIKE '%project%handover%' AND column_name IN('written_at','built_at') AND data_type='timestamp with time zone'`)
	if err != nil {
		t.Fatal(err)
	}
	targets := [][2]string{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, [2]string{table, column})
	}
	rows.Close()
	if len(targets) != 1 {
		t.Fatal("time-travel adapter needs exact persisted handover clock", targets)
	}
	// Owned fixture precondition; retain the previous good content and usage history.
	table, column := pgx.Identifier{targets[0][0]}.Sanitize(), pgx.Identifier{targets[0][1]}.Sanitize()
	phase26Exec(t, f.phase26LoadedFixture, "UPDATE "+table+" SET "+column+"="+column+"-interval '2 hours' WHERE owner_id=$1", f.Scope.OwnerID)
}
func TestPhase3H2InvalidEvidenceRejectedCountedWithoutLosingValidSentences(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	m := phase3NewModel(t, f)
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage != "project_handover" {
			return out
		}
		var wire map[string]any
		_ = json.Unmarshal([]byte(out), &wire)
		wire["conclusion"] = append(wire["conclusion"].([]any), map[string]any{"text": "这句虚构断言没有依据。", "evidence": []any{}}, map[string]any{"text": "这句引用不存在的虚构对象。", "evidence": []any{map[string]any{"kind": "memory", "id": string(memory.NewID()), "version": 1}}})
		return string(asJSON(wire))
	}
	m.mu.Unlock()
	phase3ProcessOne(t, f, "project_handover", time.Now())
	var got phase3Handover
	phase3NewHTTP(t, f).get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &got)
	phase3ValidateEvidence(t, f, got)
	if len(got.Conclusion) != 1 {
		t.Fatal("invalid evidence stored or valid content lost", got.Conclusion)
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var observable struct {
		Overflow []struct {
			Stage, Reason string
			Count         int
		}
	}
	decodePhase3(t, health, &observable)
	omitted := 0
	for _, x := range observable.Overflow {
		if strings.Contains(x.Stage, "project") && strings.Contains(x.Stage, "handover") {
			omitted += x.Count
		}
	}
	if omitted < 2 {
		t.Fatal("rejected sentences not counted in observability", string(health))
	}
}
func TestPhase3H4G2FailedThreeSegmentsPreserveLastGoodAndPending(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	m := phase3NewModel(t, f)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	phase3AgeHandoverClock(t, f)
	h := phase3NewHTTP(t, f)
	var before phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &before)
	h.command(t, f.Context, map[string]any{"type": "updateTask", "id": p.Items[0].ID, "patch": map[string]string{"notes": "虚构输入变化，要求重新判断。"}})
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage == "project_handover" {
			return `{"conclusion":[],"blockers":[],"nextSteps":[]}`
		}
		return out
	}
	m.mu.Unlock()
	stage := phase3StageName(t, "project_handover")
	if err := phase3Schedule(f.Store, f.Context, "project_handover", time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	var previousDelay time.Duration
	var originalJob memory.ID
	for attempt := 1; attempt <= 4; attempt++ {
		j := phase26ClaimStage(t, f.phase26LoadedFixture, stage)
		if attempt == 1 {
			originalJob = j.ID
		} else if j.ID != originalJob {
			t.Fatal("failed work replaced rather than retained")
		}
		attemptAt := time.Now()
		err := phase3Process(f.Store, f.Context, "project_handover", j)
		var failed *worker.JobError
		if !errors.As(err, &failed) || failed.Code == "" || failed.NoAttempt || !failed.Until.After(attemptAt) {
			t.Fatal("bad output did not retain attempted failure/backoff", err)
		}
		delay := failed.Until.Sub(attemptAt)
		if attempt >= 3 && delay <= previousDelay {
			t.Fatal("consecutive failures did not increase backoff", previousDelay, delay)
		}
		previousDelay = delay
		if err := f.Store.Defer(f.Context, j, failed.Code, failed.Until, failed.NoAttempt); err != nil {
			t.Fatal(err)
		}
		var queuedState string
		var attempts int
		if err := f.Store.pool.QueryRow(f.Context, `SELECT state,attempts FROM memory_jobs WHERE id=$1`, j.ID).Scan(&queuedState, &attempts); err != nil || queuedState != "queued" || attempts < attempt {
			t.Fatal("failure marked processed/lost attempts", queuedState, attempts, err)
		}
		var after phase3Handover
		h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &after)
		if !after.Stale || !reflect.DeepEqual(before.Conclusion, after.Conclusion) || !reflect.DeepEqual(before.Blockers, after.Blockers) || !reflect.DeepEqual(before.NextSteps, after.NextSteps) || !reflect.DeepEqual(before.WrittenAt, after.WrittenAt) {
			t.Fatal("failed rewrite overwrote previous good result/time", before, after)
		}
		// Only the owned test clock advances; the real Claim/Process/Defer loop
		// still sees the same pending work, with actual model failures each time.
		if attempt < 4 {
			phase26Exec(t, f.phase26LoadedFixture, `UPDATE memory_jobs SET available_at=now()-interval '1 second' WHERE id=$1`, j.ID)
		}
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Stages []struct {
			Stage, LastFailure string
			Failure            int
		}
	}
	decodePhase3(t, health, &report)
	found := false
	for _, x := range report.Stages {
		if x.Stage == stage {
			found = x.Failure >= 4 && x.LastFailure != ""
		}
	}
	if !found {
		t.Fatal("four failure reasons/count absent from health", string(health))
	}
	if m.count("project_handover") != 5 {
		t.Fatal("failure not actual provider attempts")
	}
}
func TestPhase3H6CorrectionUpdatesEvidenceAndStalesProject(t *testing.T) {
	phase3Finding(t, "S-P3-003")
	f := phase3LoadFixture(t)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	h := phase3NewHTTP(t, f)
	h.command(t, f.Context, map[string]any{"type": "updateTask", "id": p.Items[0].ID, "patch": map[string]string{"status": "waiting"}})
	m := phase3NewModel(t, f)
	m.Override = func(stage, out string) string {
		if stage == "project_handover" {
			return strings.ReplaceAll(out, "虚构卡点已经解决，该事项状态为done。", "虚构运输卡点尚未解决，该事项状态为waiting。")
		}
		return out
	}
	phase3ProcessOne(t, f, "project_handover", time.Now())
	var before phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &before)
	if len(before.Blockers) == 0 || !strings.Contains(before.Blockers[0].Text, "waiting") {
		t.Fatal("correction precondition must be an unresolved blocker")
	}
	m.mu.Lock()
	m.Override = func(stage, out string) string {
		if stage == "secretary" {
			return `{"reply":"已改虚构事项状态","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[{"op":"update","ref":"THIS","set":{"status":"done"}}],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`
		}
		return out
	}
	m.mu.Unlock()
	code, raw, err := h.call(f.Context, "POST", "/v1/desk/turn", map[string]any{"requestId": string(memory.NewID()), "agentId": "phase3", "thingId": p.Items[0].ID, "text": "卡点不对，那个问题上周解决了。"})
	if err != nil || code != 200 {
		t.Fatal(code, string(raw), err)
	}
	var turn struct {
		Turn struct{ Receipts []struct{ Op string } }
	}
	decodePhase3(t, raw, &turn)
	if len(turn.Turn.Receipts) == 0 {
		t.Fatal("correction did not act on underlying evidence")
	}
	for _, r := range turn.Turn.Receipts {
		if r.Op != "update" {
			t.Fatal("correction directly edited handover or made another action", r.Op)
		}
	}
	var got phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &got)
	if !got.Stale || !reflect.DeepEqual(got.Blockers, before.Blockers) || !reflect.DeepEqual(got.WrittenAt, before.WrittenAt) {
		t.Fatal("correction must stale the retained handover, not directly edit it")
	}
	var status string
	if err := f.Store.pool.QueryRow(f.Context, `SELECT status FROM work_items WHERE owner_id=$1 AND id=$2`, f.Scope.OwnerID, p.Items[0].ID).Scan(&status); err != nil || status != "done" {
		t.Fatal("correction did not update the actual cited item", status, err)
	}
}

func TestPhase3H2G1SentenceOverflowHasObservableDestination(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	phase3OnlyProject(t, f, f.Gold.Projects[0].ID)
	m := phase3NewModel(t, f)
	const proposed = 200
	m.Override = func(stage, out string) string {
		if stage != "project_handover" {
			return out
		}
		var wire map[string]any
		if err := json.Unmarshal([]byte(out), &wire); err != nil {
			return `{}`
		}
		first := wire["conclusion"].([]any)[0]
		many := make([]any, proposed)
		for i := range many {
			many[i] = first
		}
		wire["conclusion"] = many
		return string(asJSON(wire))
	}
	phase3ProcessOne(t, f, "project_handover", time.Now())
	var got phase3Handover
	phase3NewHTTP(t, f).get(t, f.Context, phase3ProjectPath(f.Gold.Projects[0].ID, "handover"), &got)
	phase3ValidateEvidence(t, f, got)
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Overflow []struct {
			Stage, Reason string
			Count         int
		}
	}
	decodePhase3(t, health, &report)
	omitted := 0
	for _, row := range report.Overflow {
		if strings.Contains(row.Stage, "project") && strings.Contains(row.Stage, "handover") {
			omitted += row.Count
		}
	}
	if len(got.Conclusion) == 0 || len(got.Conclusion) >= proposed || len(got.Conclusion)+omitted < proposed {
		t.Fatal("section limit has no accounted destination", len(got.Conclusion), omitted, string(health))
	}
}
