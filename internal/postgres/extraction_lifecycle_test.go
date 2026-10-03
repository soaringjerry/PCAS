package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func directExtractedItem(text string) extractedItem {
	return extractedItem{Kind: "memory", Text: text, Nature: "fact", Subject: "用户", Predicate: "交付约定", Quote: text, Confidence: 0.99, Acquisition: "direct"}
}

func TestExtractionConfirmationRequiresCurrentVerbatimCapture(t *testing.T) {
	baseSource := memory.SourceResult{Source: memory.Source{Connector: "capture"}}
	baseItem := directExtractedItem("月光列车周五交付")
	tests := []struct {
		name   string
		source memory.SourceResult
		item   extractedItem
		want   string
	}{
		{"direct capture without task explicit", baseSource, baseItem, "adopted"},
		{"user role capture", memory.SourceResult{Source: baseSource.Source, Context: &memory.SourceContext{Role: "user"}}, baseItem, "adopted"},
		{"imported user history", memory.SourceResult{Source: memory.Source{Connector: "archive"}, Context: &memory.SourceContext{Role: "user"}}, baseItem, "candidate"},
		{"third party source", memory.SourceResult{Source: memory.Source{Connector: "webhook"}}, baseItem, "candidate"},
		{"assistant role", memory.SourceResult{Source: baseSource.Source, Context: &memory.SourceContext{Role: "assistant"}}, baseItem, "candidate"},
		{"historical branch", memory.SourceResult{Source: baseSource.Source, Context: &memory.SourceContext{Role: "user", Branch: "historical"}}, baseItem, "candidate"},
	}
	for _, change := range []struct {
		name string
		edit func(*extractedItem)
		want string
	}{
		{"inference", func(i *extractedItem) { i.Acquisition = "inferred" }, "candidate"},
		{"reported speech", func(i *extractedItem) { i.Acquisition = "reported" }, "candidate"},
		{"missing provenance", func(i *extractedItem) { i.Acquisition = "" }, "candidate"},
		{"low confidence", func(i *extractedItem) { i.Confidence = 0.5 }, "candidate"},
		{"confidence 0.79", func(i *extractedItem) { i.Confidence = 0.79 }, "candidate"},
		{"confidence 0.8", func(i *extractedItem) { i.Confidence = 0.8 }, "adopted"},
		{"unresolved subject", func(i *extractedItem) { i.Subject = "" }, "adopted"},
		{"unresolved predicate", func(i *extractedItem) { i.Predicate = "" }, "adopted"},
		{"paraphrased assertion", func(i *extractedItem) { i.Text = "月光列车已经交付" }, "adopted"},
	} {
		item := baseItem
		change.edit(&item)
		tests = append(tests, struct {
			name   string
			source memory.SourceResult
			item   extractedItem
			want   string
		}{change.name, baseSource, item, change.want})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractionConfirmation(tc.source, tc.item); got != tc.want {
				t.Fatalf("confirmation = %q, want %q", got, tc.want)
			}
		})
	}
}

func extractionTestModel(t *testing.T, s *Store, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "测试模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
}

func writeExtractionResponse(w http.ResponseWriter, item extractedItem) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(asJSON(extracted{Items: []extractedItem{item}}))}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
}

func TestDirectCaptureEntersDefaultRunAsSourceBacked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	text := "月光列车的交付暗号是蓝色灯塔"
	var calls atomic.Int32
	extractionTestModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeExtractionResponse(w, directExtractedItem(text))
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
	src := st.Candidates[0].Source
	ref := memory.Ref{ID: memory.ID(src.SourceID), Version: src.Version, Kind: memory.SourceKind}
	if err := s.ProcessExtraction(ctx, leaseStage(t, s, scope, ref, "source.extract")); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil || len(st.Memories) != 1 {
		t.Fatalf("memory snapshot: %+v %v", st.Memories, err)
	}
	m := st.Memories[0]
	if m.Epistemic != "sourced" || m.Confirmation != "adopted" || m.Acquisition != "direct" {
		t.Fatalf("source-backed assertion mislabeled: %+v", m)
	}
	for _, a := range st.Agents {
		if a.IncludeInferred {
			t.Fatal("test requires default conservative agents")
		}
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "查询月光列车"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "manual", Kind: "ask", Prompt: "月光列车的交付暗号是什么"})
	run := st.Runs[0]
	if !strings.Contains(run.Brief, text) || !oneOf(m.ID, run.ContextMemoryIDs...) {
		t.Fatal("current direct capture missing from default assistant context", run.Brief)
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return verifyRunTx(ctx, tx, scope, run) }); err != nil {
		t.Fatal("source-backed run rejected during validation", err)
	}
	if calls.Load() != 1 {
		t.Fatal("test unexpectedly called a model outside extraction", calls.Load())
	}
}

func rememberExtractedForTest(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, text string) memory.Ref {
	t.Helper()
	ctx := context.Background()
	var ref memory.Ref
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		ref, err = s.rememberTx(ctx, tx, scope, statement{Text: text, Nature: "fact", Subject: "用户", Predicate: "交付约定", Confirmation: "candidate", Acquisition: "inferred", Actor: "ai", Quote: text, Source: source})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func claimIsApplicable(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, known time.Time) bool {
	t.Helper()
	var exists bool
	err := s.pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM applicable_claim_versions($1,now(),$3) WHERE claim_id=$2)", string(scope.OwnerID), string(ref.ID), known).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestSourceReplacementRetainsHistoryAndRefreshesRepeatedEvidence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	oldText, stableText, newText := "月光列车周五交付", "交付暗号是蓝色灯塔", "月光列车周一交付"
	in := input()
	in.Text = oldText + "\n" + stableText
	src1 := mustIngest(t, s, scope, in)
	old := rememberExtractedForTest(t, s, scope, src1.Ref, oldText)
	stable := rememberExtractedForTest(t, s, scope, src1.Ref, stableText)
	var knownBefore time.Time
	if err := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&knownBefore); err != nil {
		t.Fatal(err)
	}
	in.ExternalVersion = "v2"
	in.Text = newText + "\n" + stableText
	src2 := mustIngest(t, s, scope, in)
	if claimIsApplicable(t, s, scope, old, time.Now()) || claimIsApplicable(t, s, scope, stable, time.Now()) {
		t.Fatal("unsupported old extraction still current before v2 extraction")
	}
	if !claimIsApplicable(t, s, scope, old, knownBefore) {
		t.Fatal("source replacement erased historical knowledge")
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil || len(st.Memories) != 0 {
		t.Fatalf("stale source assertions still in workspace: %+v %v", st.Memories, err)
	}
	fresh := rememberExtractedForTest(t, s, scope, src2.Ref, newText)
	repeated := rememberExtractedForTest(t, s, scope, src2.Ref, stableText)
	if repeated.ID != stable.ID || repeated.Version != stable.Version {
		t.Fatal("same assertion was duplicated", stable, repeated)
	}
	if !claimIsApplicable(t, s, scope, fresh, time.Now()) || !claimIsApplicable(t, s, scope, stable, time.Now()) || claimIsApplicable(t, s, scope, old, time.Now()) {
		t.Fatal("current source evidence did not replace the old current view")
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM evidence WHERE owner_id=$1 AND target_id=$2 AND source_id=$3 AND source_version=$4", string(scope.OwnerID), string(stable.ID), string(src2.ID), src2.Version).Scan(&count); err != nil || count != 1 {
		t.Fatal("new source version missing exact supporting evidence", count, err)
	}
	rememberExtractedForTest(t, s, scope, src2.Ref, stableText)
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM evidence WHERE owner_id=$1 AND target_id=$2 AND source_id=$3 AND source_version=$4", string(scope.OwnerID), string(stable.ID), string(src2.ID), src2.Version).Scan(&count); err != nil || count != 1 {
		t.Fatal("evidence retry was not idempotent", count, err)
	}
	if out, err := s.Expand(ctx, scope, memory.ExpandRequest{Refs: []memory.Ref{old}, History: true, Evidence: true}); err != nil || len(out.Claims) != 1 {
		t.Fatal("old assertion unavailable for historical inspection", out, err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "confirmMemory", ID: string(fresh.ID)})
	in.ExternalVersion = "v3"
	in.Text = "源文档现在没有这条信息"
	mustIngest(t, s, scope, in)
	if !claimIsApplicable(t, s, scope, fresh, time.Now()) {
		t.Fatal("independently user-confirmed memory erased by source replacement")
	}
}

func TestSupersededExtractionDoesNotCommitOrCallProvider(t *testing.T) {
	for _, duringCall := range []bool{false, true} {
		name := "before provider call"
		if duringCall {
			name = "during provider call"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			scope := owner()
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var calls atomic.Int32
			extractionTestModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				close(started)
				<-release
				writeExtractionResponse(w, directExtractedItem("旧版约定周五交付"))
			})
			// Unblock the fake provider before its cleanup waits for handlers.
			t.Cleanup(unblock)
			in := input()
			in.Text = "旧版约定周五交付"
			src := mustIngest(t, s, scope, in)
			job := leaseStage(t, s, scope, src.Ref, "source.extract")
			done := make(chan error, 1)
			if duringCall {
				go func() { done <- s.ProcessExtraction(ctx, job) }()
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("fake provider did not start")
				}
			}
			in.ExternalVersion = "v2"
			in.Text = "新版约定周一交付"
			mustIngest(t, s, scope, in)
			unblock()
			if duringCall {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			} else if err := s.ProcessExtraction(ctx, job); err != nil {
				t.Fatal(err)
			}
			wantCalls := int32(0)
			if duringCall {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatal("superseded job performed unexpected provider request", calls.Load())
			}
			var count int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale extraction committed claims", count, err)
			}
			var state string
			if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE id=$1", string(job.ID)).Scan(&state); err != nil || state != "done" {
				t.Fatal("superseded extraction left retryable job", state, err)
			}
		})
	}
}
