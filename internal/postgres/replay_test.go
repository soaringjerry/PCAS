package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func workspaceCommand(t *testing.T, s *Store, scope memory.Scope, c workspace.Command) workspace.State {
	t.Helper()
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	c.RequestID = string(memory.NewID())
	c.ExpectedRevision = state.Revision
	state, err = s.Execute(context.Background(), scope, c)
	if err != nil {
		t.Fatalf("%s: %v", c.Type, err)
	}
	return state
}
func leaseStage(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, stage string) worker.Job {
	t.Helper()
	j := worker.Job{OwnerID: scope.OwnerID, Record: ref, Stage: stage, LeaseToken: memory.NewID(), Attempts: 1}
	err := s.pool.QueryRow(context.Background(), `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state,attempts,lease_token,lease_until) VALUES(gen_random_uuid(),$1,$2,$3,$4,'leased',1,$5,now()+interval '5 minutes') ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='leased',attempts=1,lease_token=$5,lease_until=now()+interval '5 minutes' RETURNING id::text`, string(scope.OwnerID), string(ref.ID), ref.Version, stage, string(j.LeaseToken)).Scan(&j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func TestExtractionEvidenceSignalAndReminderDeletion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "去旧书店"})
	idea := st.Ideas[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaShelve", ID: idea.ID, Condition: "旧书店重新开门"})
	idea = st.Ideas[0]
	src := input()
	src.Text = "旧书店今天重新营业。"
	source := mustIngest(t, s, scope, src)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := map[string]any{"items": []map[string]any{{"kind": "memory", "nature": "fact", "text": "书店重新营业", "subject": "旧书店", "predicate": "营业状态", "quote": src.Text, "confidence": 0.99, "explicit": true}, {"kind": "memory", "nature": "fact", "text": "凭空捏造", "quote": "原文不存在", "confidence": 1}}, "signals": []conditionSignal{{IdeaID: idea.ID, ConditionID: idea.Conditions[0].ID, Quote: src.Text, Explanation: "导入资料写明重新营业", Confidence: 0.99}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(asJSON(content))}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}})
	j := leaseStage(t, s, scope, source.Ref, "source.extract")
	if err := s.ProcessExtraction(ctx, j); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Memories) != 1 || st.Memories[0].Epistemic == "confirmed" || st.Ideas[0].Status != "awakened" || st.Ideas[0].Conditions[0].Met {
		t.Fatalf("extraction or inferred wake: %+v", st.Ideas)
	}
	if !strings.Contains(st.Ideas[0].Wake.Reason, "待核验") || calls != 1 {
		t.Fatal("uncertainty or call count")
	}
	if err := s.ProcessExtraction(ctx, j); !errors.Is(err, worker.ErrLeaseLost) {
		t.Fatal("completed job called again", err)
	}
	if calls != 1 {
		t.Fatal("duplicate provider submission")
	}
	if err := s.CheckReminders(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{source.Ref}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	st, err = s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if st.Ideas[0].Wake != nil || st.Ideas[0].Conditions[0].MetBy != nil || st.Ideas[0].Status != "shelved" {
		t.Fatal("deleted wake evidence survived")
	}
	var cost float64
	if err := s.pool.QueryRow(ctx, "SELECT coalesce(sum(reserved_cost),0) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil || cost <= 0 {
		t.Fatal("billing history lost", err)
	}
}
func TestRunGrantRevocationAndArtifactCleanup(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "受控的私密事实"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "受控的私密事实"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "测试事项"})
	task := st.Tasks[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: "summary", Prompt: "总结"})
	run := st.Runs[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "由私密事实推导的结果"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: "由私密事实推导的结果"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: task.ID, Patch: asJSON(map[string]any{"notes": st.Tasks[0].Notes + strings.Repeat("n", 31000)})})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: "ask", Prompt: "使用采纳内容"})
	if !oneOf(mem.ID, st.Runs[0].ContextMemoryIDs...) {
		t.Fatal("derived context lost its transitive dependency")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "manual", Patch: asJSON(map[string]any{"memoryKinds": []string{"preference"}})})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: "ask", Prompt: "只看偏好"})
	if strings.Contains(st.Runs[0].Brief, "私密事实") {
		t.Fatal("copied result bypassed kind policy")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: mem.ID, AgentIDs: []string{}})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: "ask", Prompt: "重新总结"})
	if strings.Contains(st.Runs[0].Brief, "私密事实") {
		t.Fatal("adopted copy bypassed revocation")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
	data, err := s.Export(ctx, scope, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "私密事实") {
		t.Fatal("deleted derived content survived in export")
	}
}
func TestAsyncRunInvalidatesDuringGenerationAndBudget(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "旧依据生成的结果"}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 10}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "旧依据"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "旧依据"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "测试生成"})
	task := st.Tasks[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "model", Kind: "ask", Prompt: "回答"})
	done := make(chan error, 1)
	go func() { done <- s.runAgentOnce(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("provider did not start")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: mem.ID, Text: "纠正后的依据", Reason: "原先记错"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs[0].Status != "failed" || !st.Runs[0].StaleContext || st.Runs[0].Output != "" {
		t.Fatal("stale response exposed")
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 0})})
	_, err = s.Execute(ctx, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "model", Kind: "ask", Prompt: "再次", RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
	if !errors.Is(err, workspace.ErrBudget) {
		t.Fatal("budget bypass", err)
	}
}
func TestLongExtractionSchedulesEveryWindow(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	s.SetModels(&ai.Registry{Config: ai.Configuration{Extraction: "placeholder"}})
	in := input()
	in.Text = strings.Repeat("原", 36000) + "尾部线索"
	src := mustIngest(t, s, scope, in)
	job := leaseStage(t, s, scope, src.Ref, "source.extract")
	if err := s.ProcessExtraction(ctx, job); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'source.extract:%'", string(scope.OwnerID), string(src.ID)).Scan(&count); err != nil || count != 4 {
		t.Fatal("missing tail window", count, err)
	}
}
func TestExplicitTimeReminderIsIdempotent(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "跟进"})
	item := st.Tasks[0]
	item.Triggers = []workspace.Trigger{{ID: string(memory.NewID()), Kind: "time", NextAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), Active: true, Description: "到时间跟进", Guard: "todo"}}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return saveItem(ctx, tx, scope, item) }); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.CheckReminders(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM workspace_notices WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate reminder", count, err)
	}
}

func TestTemporalChangeCorrectionAndHistoricalKnowledge(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "我住在河边"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "我住在河边"})
	ref := memory.Ref{ID: memory.ID(st.Memories[0].ID), Kind: memory.ClaimKind, Version: 1}
	expanded, err := s.Expand(ctx, scope, memory.ExpandRequest{Refs: []memory.Ref{ref}})
	if err != nil {
		t.Fatal(err)
	}
	claim := expanded.Claims[0]
	knownBefore := time.Now()
	movedAt := knownBefore.Add(-time.Hour)
	claim.Value = asJSON("搬到山边")
	claim.ValidTime = memory.TimeRange{From: &movedAt, Precision: "instant"}
	changed, err := s.Correct(ctx, scope, memory.CorrectRequest{Target: ref, Replacement: claim, ChangeType: "change", Reason: "真实搬家"})
	if err != nil {
		t.Fatal(err)
	}
	knownChange := time.Now()
	claim.Value = asJSON("搬到湖边")
	corrected, err := s.Correct(ctx, scope, memory.CorrectRequest{Target: changed, Replacement: claim, Reason: "把山边记错了"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		valid, known time.Time
		version      int
	}{{movedAt.Add(-time.Hour), time.Now(), 1}, {time.Now(), knownBefore, 1}, {time.Now(), knownChange, 2}, {time.Now(), time.Now(), corrected.Version}} {
		var version int
		if err := s.pool.QueryRow(ctx, "SELECT version FROM applicable_claim_versions($1,$2,$3) WHERE claim_id=$4", string(scope.OwnerID), test.valid, test.known, string(ref.ID)).Scan(&version); err != nil || version != test.version {
			t.Fatal("temporal view", version, test.version, err)
		}
	}
}
