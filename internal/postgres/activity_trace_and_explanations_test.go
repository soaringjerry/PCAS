package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type executionTraceResult struct {
	AnchorID string `json:"anchorId"`
	Calls    []struct {
		ID              string         `json:"id"`
		ExecutionID     string         `json:"executionId"`
		PromptName      string         `json:"promptName"`
		InstructionHash string         `json:"instructionHash"`
		UsageID         string         `json:"usageId"`
		Outcome         string         `json:"outcome"`
		InputManifest   map[string]any `json:"inputManifest"`
	} `json:"calls"`
	Events []struct {
		ID         string `json:"id"`
		Stage      string `json:"stage"`
		JobID      string `json:"jobId"`
		RootID     string `json:"rootId"`
		CauseID    string `json:"causeId"`
		ChainDepth int    `json:"chainDepth"`
		JobState   string `json:"jobState"`
	} `json:"events"`
	Actions []struct {
		ID      string   `json:"id"`
		Source  string   `json:"source"`
		Summary string   `json:"summary"`
		Items   []string `json:"items"`
	} `json:"actions"`
	Runs   []map[string]any `json:"runs"`
	Stages []map[string]any `json:"stages"`
	Gaps   []string         `json:"gaps"`
}

type explanationsResult struct {
	PausedJobs []struct {
		JobID                 string `json:"jobId"`
		BaseStage             string `json:"baseStage"`
		State                 string `json:"state"`
		ErrorCode             string `json:"errorCode"`
		ChainDepth            int    `json:"chainDepth"`
		RecoveryState         string `json:"recoveryState"`
		RequiresOwnerRecovery bool   `json:"requiresOwnerRecovery"`
	} `json:"pausedJobs"`
	TopicDecisions []struct {
		TopicID   string `json:"topicId"`
		TopicName string `json:"topicName"`
		Decision  string `json:"decision"`
		Reason    string `json:"reason"`
	} `json:"topicDecisions"`
	DateDecisions []struct {
		ClaimID    string `json:"claimId"`
		Class      string `json:"class"`
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
		EntryTitle string `json:"entryTitle"`
	} `json:"dateDecisions"`
}

func readExecutionTrace(t *testing.T, s *Store, scope memory.Scope, id string) executionTraceResult {
	t.Helper()
	raw, err := s.ExecutionTrace(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var out executionTraceResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err, string(raw))
	}
	return out
}

func readExplanations(t *testing.T, s *Store, scope memory.Scope) explanationsResult {
	t.Helper()
	raw, err := s.ActivityExplanations(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	var out explanationsResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err, string(raw))
	}
	return out
}

func TestExecutionTraceLinksActionToCallAndCausalJobChain(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Recorded.","actions":[{"op":"create_task","title":"Traceable task"}]}`)
	})

	req := turnRequest("Please track Traceable task.")
	turn := mustTurn(t, s, scope, req).Turn
	if len(turn.Receipts) != 1 || turn.Receipts[0].ActionID == nil {
		t.Fatalf("unexpected receipts: %+v", turn.Receipts)
	}
	actionID := *turn.Receipts[0].ActionID

	// Tracing by actionId resolves back to the secretary turn's executionId and its model call.
	trace := readExecutionTrace(t, s, scope, actionID)
	if len(trace.Actions) != 1 || trace.Actions[0].ID != actionID || len(trace.Actions[0].Items) != 1 {
		t.Fatalf("expected action with item ID, got %+v", trace.Actions)
	}
	if len(trace.Calls) == 0 || trace.Calls[0].ExecutionID != req.RequestID || trace.Calls[0].PromptName == "" || len(trace.Calls[0].InstructionHash) != 64 {
		t.Fatalf("expected linked model call with prompt hash, got %+v", trace.Calls)
	}
	if len(trace.Gaps) == 0 {
		t.Fatalf("expected explicit privacy/link gap note")
	}

	// Tracing by a queued job ID returns its full recorded_events causal chain.
	source, err := s.Ingest(ctx, scope, memory.IngestRequest{Connector: "note", ExternalID: "trace-src", ExternalVersion: "1", Title: "Trace note", Text: "Remember this.", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 ORDER BY created_at LIMIT 1`, string(scope.OwnerID), string(source.ID)).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	jobTrace := readExecutionTrace(t, s, scope, jobID)
	if len(jobTrace.Events) == 0 || jobTrace.Events[0].JobID != jobID || jobTrace.Events[0].ChainDepth != 0 {
		t.Fatalf("expected recorded_events entry for job %s, got %+v", jobID, jobTrace.Events)
	}

	// Owner isolation and validation.
	if other := readExecutionTrace(t, s, owner(), actionID); len(other.Actions) != 0 || len(other.Calls) != 0 {
		t.Fatalf("another owner saw trace rows: %+v", other)
	}
	if _, err := s.ExecutionTrace(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}, actionID); err == nil {
		t.Fatal("non-owner read execution trace")
	}
	if _, err := s.ExecutionTrace(ctx, scope, "not-a-uuid"); err == nil {
		t.Fatal("invalid UUID accepted")
	}
}

func TestActivityExplanationsExposePausedRecoveryTopicSkipsAndDateDecisions(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	source, err := s.Ingest(ctx, scope, memory.IngestRequest{Connector: "note", ExternalID: "exp-src", ExternalVersion: "1", Title: "Explain note", Text: "Old trip to Chengdu.", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 ORDER BY created_at LIMIT 1`, string(scope.OwnerID), string(source.ID)).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	// Mark the job failed with chain_limit so it requires explicit owner recovery.
	if _, err := s.Pool.Exec(ctx, `UPDATE memory_jobs SET state='failed',error_code='chain_limit',chain_depth=201 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), jobID); err != nil {
		t.Fatal(err)
	}
	// Insert a topic entity + skipped topic_project_checks row and a date_tidy_checks decision.
	topicID := string(memory.NewID())
	if err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO memory_records(owner_id,id,kind,version,state) VALUES($1,$2,'entity',1,'active')`, []any{string(scope.OwnerID), topicID}},
			{`INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)`, []any{string(scope.OwnerID), topicID}},
			{`INSERT INTO entities(owner_id,id) VALUES($1,$2)`, []any{string(scope.OwnerID), topicID}},
			{`INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,'topic','Past Chengdu trip')`, []any{string(scope.OwnerID), topicID}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO topic_project_checks(owner_id,topic_id,input_hash,page,output,evidence_hash) VALUES($1,$2,'h1',0,'{"decision":"skip","reason":"Trip already ended last year"}','e1')`, string(scope.OwnerID), topicID); err != nil {
		t.Fatal(err)
	}
	claimID := string(memory.NewID())
	if _, err := s.Pool.Exec(ctx, `INSERT INTO date_tidy_checks(owner_id,claim_id,claim_version,class,decision,reason,entry_title) VALUES($1,$2,1,'past','drop','Event is in the past','Chengdu flight')`, string(scope.OwnerID), claimID); err != nil {
		t.Fatal(err)
	}

	exp := readExplanations(t, s, scope)
	if len(exp.PausedJobs) != 1 || exp.PausedJobs[0].JobID != jobID || !exp.PausedJobs[0].RequiresOwnerRecovery || exp.PausedJobs[0].ChainDepth != 201 {
		t.Fatalf("unexpected paused jobs: %+v", exp.PausedJobs)
	}
	if len(exp.TopicDecisions) != 1 || exp.TopicDecisions[0].TopicName != "Past Chengdu trip" || exp.TopicDecisions[0].Decision != "skip" || exp.TopicDecisions[0].Reason == "" {
		t.Fatalf("unexpected topic decisions: %+v", exp.TopicDecisions)
	}
	if len(exp.DateDecisions) != 1 || exp.DateDecisions[0].ClaimID != claimID || exp.DateDecisions[0].Decision != "drop" {
		t.Fatalf("unexpected date decisions: %+v", exp.DateDecisions)
	}

	// Owner retryJob command re-queues the failed job and clears it from pausedJobs.
	workspaceCommand(t, s, scope, workspace.Command{Type: "retryJob", ID: jobID})
	after := readExplanations(t, s, scope)
	if len(after.PausedJobs) != 0 {
		t.Fatalf("expected paused job cleared after owner retryJob, got %+v", after.PausedJobs)
	}

	// Owner isolation.
	if other := readExplanations(t, s, owner()); len(other.PausedJobs) != 0 || len(other.TopicDecisions) != 0 || len(other.DateDecisions) != 0 {
		t.Fatalf("another owner saw explanations: %+v", other)
	}
	if _, err := s.ActivityExplanations(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}); err == nil {
		t.Fatal("non-owner read explanations")
	}
}
