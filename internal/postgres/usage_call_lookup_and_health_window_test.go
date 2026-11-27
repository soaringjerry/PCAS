package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestBackgroundHealthLastFailureRespectsOneHourWindow(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	if _, err := s.Pool.Exec(ctx,
		`INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count,at) VALUES($1,'memory.organize','failure','schedule_background_write_busy',1,now()-interval '2 hours')`,
		string(scope.OwnerID),
	); err != nil {
		t.Fatal(err)
	}
	readStage := func(stage string) (int, string) {
		t.Helper()
		raw, err := s.BackgroundHealth(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Stages []struct {
				Stage       string `json:"stage"`
				Failure     int    `json:"failure"`
				LastFailure string `json:"lastFailure"`
			} `json:"stages"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		for _, st := range body.Stages {
			if st.Stage == stage {
				return st.Failure, st.LastFailure
			}
		}
		t.Fatalf("stage %s not found in health", stage)
		return 0, ""
	}
	if failures, last := readStage("memory.organize"); failures != 0 || last != "" {
		t.Fatalf("expected old failure outside 1h window omitted, got failures=%d lastFailure=%q", failures, last)
	}
	if _, err := s.Pool.Exec(ctx,
		`INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count,at) VALUES($1,'memory.organize','failure','model_call_failed',1,now()-interval '5 minutes')`,
		string(scope.OwnerID),
	); err != nil {
		t.Fatal(err)
	}
	if failures, last := readStage("memory.organize"); failures != 1 || last != "model_call_failed" {
		t.Fatalf("expected recent failure inside 1h window, got failures=%d lastFailure=%q", failures, last)
	}
}

func TestDayActivitySummaryCoversWholeDayAndExcludesOverflowAndReplaced(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: kernel.JSON(map[string]string{"timezone": "UTC"})})
	now := time.Now().UTC()
	today := now.Format(time.DateOnly)

	// Insert: 1 active failed call, 1 replaced failed call, 1 deferred stage, 3 normal overflow events.
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{
			`INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,attempt_number,attempt_limit,function_name,stage,provider_id,model,input_manifest,outcome,error_code,accounting_state,recovery_state,created_at,finished_at)
			 VALUES($1,gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),1,1,'query_embedding','query','embed','m','{}'::jsonb,'failed','model_call_failed','not_reserved','active',$2,$2)`,
			[]any{string(scope.OwnerID), now},
		},
		{
			`INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,attempt_number,attempt_limit,function_name,stage,provider_id,model,input_manifest,outcome,error_code,accounting_state,recovery_state,recovery_reason,created_at,finished_at)
			 VALUES($1,gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),1,2,'secretary','answer','chatgpt','m','{}'::jsonb,'failed','model_call_failed','not_reserved','replaced','secretary_transient_retry',$2,$2)`,
			[]any{string(scope.OwnerID), now},
		},
		{
			`INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count,at) VALUES($1,'memory.topic_project','deferred','topic_project_hourly_limit',1,$2)`,
			[]any{string(scope.OwnerID), now},
		},
		{
			`INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count,at) VALUES($1,'secretary','overflow','recall_budget',5,$2),($1,'secretary','overflow','source_recall_budget',3,$2),($1,'secretary','overflow','history_context_limit',2,$2)`,
			[]any{string(scope.OwnerID), now},
		},
	} {
		if _, err := s.Pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	// Paginate with limit=1 so omittedEvents > 0, and verify summary still reports full-day totals.
	raw, err := s.DayActivity(ctx, scope, today, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		EventCount    int `json:"eventCount"`
		OmittedEvents int `json:"omittedEvents"`
		Summary       struct {
			Failures  int `json:"failures"`
			Deferrals int `json:"deferrals"`
			Overflows int `json:"overflows"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.EventCount != 6 || out.OmittedEvents != 5 {
		t.Fatalf("unexpected paging counts: %+v", out)
	}
	if out.Summary.Failures != 1 || out.Summary.Deferrals != 1 || out.Summary.Overflows != 3 {
		t.Fatalf("expected failures=1 (excluding overflow and replaced), deferrals=1, overflows=3; got %+v", out.Summary)
	}
}

func TestUsageSummaryRangeMedianAndUsageCallByID(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: kernel.JSON(map[string]string{"timezone": "UTC"})})

	// Day 1: 3 calls of 100ms (daily median = 100). Day 2: 1 call of 1000ms (daily median = 1000).
	// Average of daily medians would be 550ms; true range median across [100, 100, 100, 1000] is 100ms.
	source, err := s.Ingest(ctx, scope, memory.IngestRequest{Connector: "note", ExternalID: "u-ref", ExternalVersion: "1", Title: "Note", Text: "Referenced source body", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	olderCallID := string(memory.NewID())
	for _, row := range []struct {
		id  string
		at  string
		ms  int64
		ref []memory.Ref
	}{
		{olderCallID, "2025-02-01T08:00:00Z", 100, []memory.Ref{{ID: source.ID, Version: 1, Kind: memory.SourceKind}}},
		{string(memory.NewID()), "2025-02-01T09:00:00Z", 100, nil},
		{string(memory.NewID()), "2025-02-01T10:00:00Z", 100, nil},
		{string(memory.NewID()), "2025-02-03T08:00:00Z", 1000, nil},
	} {
		refs := row.ref
		if refs == nil {
			refs = []memory.Ref{}
		}
		if _, err := s.Pool.Exec(ctx,
			`INSERT INTO model_usage(owner_id,id,at,purpose,agent_id,model,input_tokens,output_tokens,cost,memory_refs,duration_ms)
			 VALUES($1,$2,$3,'secretary','chatgpt','gpt-fake',100,20,0.01,$4,$5)`,
			string(scope.OwnerID), row.id, row.at, kernel.JSON(refs), row.ms,
		); err != nil {
			t.Fatal(err)
		}
	}

	rawSummary, err := s.UsageSummary(ctx, scope, "2025-02-01", "2025-02-04")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Days     []struct{ Date string } `json:"days"`
		Purposes []struct {
			Purpose    string `json:"purpose"`
			Calls      int    `json:"calls"`
			DurationMs struct {
				Median float64 `json:"median"`
				Max    float64 `json:"max"`
			} `json:"durationMs"`
		} `json:"purposes"`
	}
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Days) != 2 {
		t.Fatalf("expected days to keep omitting empty days, got %+v", summary.Days)
	}
	if len(summary.Purposes) != 1 || summary.Purposes[0].Calls != 4 || summary.Purposes[0].DurationMs.Median != 100 || summary.Purposes[0].DurationMs.Max != 1000 {
		t.Fatalf("expected true range median=100 max=1000 across 4 calls, got %+v", summary.Purposes)
	}

	// Direct lookup by ID resolves referenced source text and enforces owner isolation.
	rawCall, err := s.UsageCallByID(ctx, scope, olderCallID)
	if err != nil {
		t.Fatal(err)
	}
	var call struct {
		ID   string `json:"id"`
		Refs []struct {
			ID      string `json:"id"`
			Text    string `json:"text"`
			Deleted bool   `json:"deleted"`
		} `json:"refs"`
	}
	if err := json.Unmarshal(rawCall, &call); err != nil {
		t.Fatal(err)
	}
	if call.ID != olderCallID || len(call.Refs) != 1 || call.Refs[0].Deleted || call.Refs[0].Text != "Referenced source body" {
		t.Fatalf("unexpected direct call lookup: %+v", call)
	}
	if _, err := s.UsageCallByID(ctx, owner(), olderCallID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another owner, got %v", err)
	}
	if _, err := s.UsageCallByID(ctx, scope, "not-a-uuid"); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for malformed UUID, got %v", err)
	}
}
