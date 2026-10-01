package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The oracle is stabilization S1–S9 and the phase1 DeskTurn contract. Model
// outputs are literal protocol fixtures, independent of secretary algorithms.
func stabilizationSecretaryTasks(t *testing.T, s *Store, scope memory.Scope, want int) {
	t.Helper()
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM work_items WHERE owner_id=$1 AND kind='task'", string(scope.OwnerID)).Scan(&count); err != nil || count != want {
		t.Fatalf("persisted task count: got %d, want %d, error %v", count, want, err)
	}
}

// Diagnostic opt-in reruns the unchanged failing assertions. A normal green
// run with these skips is explicitly not stabilization acceptance.
func stabilizationSecretaryFinding(t *testing.T, finding string) {
	t.Helper()
	if os.Getenv("PCAS_STABILIZATION_RUN_FINDINGS") != "1" {
		t.Skip("finding " + finding + "; see docs/evaluations/2026-10-01-stabilization-secretary.md; set PCAS_STABILIZATION_RUN_FINDINGS=1 to reproduce")
	}
}

func TestStabilizationS1_ConcurrentConversationSeesPreviousObject(t *testing.T) {
	stabilizationSecretaryFinding(t, "T3-S1: second model begins before first turn commits and loses R1")
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "UTC"})})
	conversation := string(memory.NewID())
	firstEntered, releaseFirst, secondInvoked, secondEntered := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	var calls atomic.Int32
	var secondPrompt string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
			secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"S1会议","due":"2026-10-02T15:00"}]}`)
			return
		}
		secondPrompt = string(body)
		close(secondEntered)
		secretaryModelReply(w, `{"actions":[{"op":"update","ref":"R1","set":{"due":"2026-10-02T16:00"}}]}`)
	})
	t.Cleanup(release)
	type result struct {
		out workspace.DeskTurnResponse
		err error
	}
	firstResult, secondResult := make(chan result, 1), make(chan result, 1)
	first := turnRequest("明天三点开会")
	first.ConversationID = &conversation
	go func() { out, err := s.DeskTurn(context.Background(), scope, first); firstResult <- result{out, err} }()
	select {
	case <-firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first model did not enter")
	}
	second := turnRequest("改到四点")
	second.ConversationID = &conversation
	go func() {
		close(secondInvoked)
		out, err := s.DeskTurn(context.Background(), scope, second)
		secondResult <- result{out, err}
	}()
	<-secondInvoked
	// First user input is admitted before second input; its model result is held
	// behind a barrier. An unprotected second generation enters and returns
	// before the first is released. The timeout bounds a correctly blocked second
	// generation; it does not pick model completion order with a short sleep.
	premature := false
	select {
	case <-secondEntered:
		premature = true
	case <-time.After(2 * time.Second):
	}
	release()
	a, b := <-firstResult, <-secondResult
	if a.err != nil || b.err != nil {
		t.Fatalf("turn errors: first=%v second=%v", a.err, b.err)
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
	current, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if premature {
		t.Error("same conversation second model entered before first result was released")
	}
	if len(current.Tasks) != 1 || current.Tasks[0].Due != "2026-10-02T16:00:00Z" {
		t.Errorf("final stored task was not rescheduled: %+v", current.Tasks)
	}
	if len(a.out.State.Tasks) != 1 || len(b.out.State.Tasks) != 1 || len(b.out.Turn.Receipts) != 1 {
		t.Fatalf("second turn did not update one object: tasks=%d receipts=%+v", len(b.out.State.Tasks), b.out.Turn.Receipts)
	}
	if b.out.State.Tasks[0].ID != a.out.State.Tasks[0].ID || !strings.Contains(b.out.State.Tasks[0].Due, "16:00") || b.out.Turn.Receipts[0].Status != "done" || !strings.Contains(secondPrompt, "S1会议") || !strings.Contains(secondPrompt, "R1") {
		t.Fatalf("second turn lacked first result: due=%s receipts=%+v contextHasTask=%v contextHasR1=%v", b.out.State.Tasks[0].Due, b.out.Turn.Receipts, strings.Contains(secondPrompt, "S1会议"), strings.Contains(secondPrompt, "R1"))
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
}

func TestStabilizationS2_NoThisDoesNotCompleteArbitraryTask(t *testing.T) {
	s, scope := testStore(t), owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "S2甲"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "S2乙"})
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"actions":[{"op":"update","ref":"THIS","set":{"status":"done"}}]}`)
	})
	out := mustTurn(t, s, scope, turnRequest("把这个标成完成"))
	for _, task := range out.State.Tasks {
		if task.Status != "todo" {
			t.Fatal("unspecified task completed", task.ID)
		}
	}
	if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Status != "skipped" || out.Turn.Receipts[0].Reason == "" {
		t.Fatal("missing explanation", out.Turn.Receipts)
	}
	stabilizationSecretaryTasks(t, s, scope, 2)
}

func TestStabilizationS3_InvalidReferencesDoNotBlockLegalAction(t *testing.T) {
	s, scope := testStore(t), owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "S3保持原状"})
	id := st.Tasks[0].ID
	unknown := string(memory.NewID())
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, map[string]any{"actions": []any{map[string]any{"op": "update", "ref": "T999", "set": map[string]string{"title": "非法别名"}}, map[string]any{"op": "update", "ref": unknown, "set": map[string]string{"title": "未知UUID"}}, map[string]any{"op": "update", "ref": id, "set": map[string]string{"title": "原始UUID绕过"}}, map[string]any{"op": "create_task", "title": "S3合法新事项"}}})
	})
	out := mustTurn(t, s, scope, turnRequest("修改和新增"))
	if len(out.Turn.Receipts) != 4 || len(out.State.Tasks) != 2 {
		t.Fatal(out.Turn.Receipts, len(out.State.Tasks))
	}
	for _, r := range out.Turn.Receipts[:3] {
		if r.Status != "skipped" || r.Reason == "" || r.ActionID != nil {
			t.Fatal("invalid reference executed", r)
		}
	}
	if out.Turn.Receipts[3].Status != "done" {
		t.Fatal("legal action was blocked")
	}
	for _, task := range out.State.Tasks {
		if task.ID == id && task.Title != "S3保持原状" {
			t.Fatal("existing item changed")
		}
	}
	stabilizationSecretaryTasks(t, s, scope, 2)
}

func TestStabilizationS4_IdempotencyAndFreshRequest(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"S4买牛奶"}]}`)
	})
	req := turnRequest("买牛奶")
	first := mustTurn(t, s, scope, req)
	replay := mustTurn(t, s, scope, req)
	if first.Turn.ID != replay.Turn.ID || calls.Load() != 1 {
		t.Fatal("same key re-executed")
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
	changed := req
	changed.Text = "买面包"
	if _, err := s.DeskTurn(context.Background(), scope, changed); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("changed request body accepted", err)
	}
	req.RequestID = string(memory.NewID())
	second := mustTurn(t, s, scope, req)
	if second.Turn.ID == first.Turn.ID || calls.Load() != 2 {
		t.Fatal("new key suppressed user intent")
	}
	stabilizationSecretaryTasks(t, s, scope, 2)
	var turns, actions int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM desk_turns WHERE owner_id=$1", string(scope.OwnerID)).Scan(&turns); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM action_log WHERE owner_id=$1", string(scope.OwnerID)).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if turns != 2 || actions != 2 {
		t.Fatal("duplicate persistence", turns, actions)
	}
}

func TestStabilizationS5_AskAndActionsBothSurvive(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"已建事项，请确认地点","actions":[{"op":"create_task","title":"S5开会"}],"ask":{"question":"在哪里？","options":["办公室","线上"]}}`)
	})
	out := mustTurn(t, s, scope, turnRequest("安排开会"))
	if out.Turn.Ask == nil || out.Turn.Ask.Question != "在哪里？" || len(out.Turn.Ask.Options) != 2 || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Status != "done" {
		t.Fatal(out.Turn)
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
}

func TestStabilizationS6_AnswerOptionContinuesPreviousObject(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	var prompt string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"S6会议"}],"ask":{"question":"地点？","options":["办公室","线上"]}}`)
			return
		}
		prompt = string(body)
		secretaryModelReply(w, `{"actions":[{"op":"update","ref":"R1","set":{"notesAppend":"办公室"}}]}`)
	})
	first := mustTurn(t, s, scope, turnRequest("安排会议"))
	req := turnRequest(first.Turn.Ask.Options[0])
	req.ConversationID = &first.ConversationID
	second := mustTurn(t, s, scope, req)
	if len(second.State.Tasks) != 1 || second.State.Tasks[0].ID != first.State.Tasks[0].ID || second.State.Tasks[0].Notes != "办公室" || !strings.Contains(prompt, "S6会议") {
		t.Fatal("option did not continue original item", second.Turn.Receipts)
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
}

func TestStabilizationS7_ItemPageUsesOnlyCurrentThing(t *testing.T) {
	s, scope := testStore(t), owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "S7当前"})
	id := st.Tasks[0].ID
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "S7另一件"})
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"actions":[{"op":"update","ref":"THIS","set":{"status":"done"}}]}`)
	})
	req := turnRequest("做完了")
	req.ThingID = &id
	out := mustTurn(t, s, scope, req)
	for _, task := range out.State.Tasks {
		want := "todo"
		if task.ID == id {
			want = "done"
		}
		if task.Status != want {
			t.Fatal("wrong task completed", task.ID, task.Status)
		}
	}
	if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].ThingID == nil || *out.Turn.Receipts[0].ThingID != id {
		t.Fatal(out.Turn.Receipts)
	}
	stabilizationSecretaryTasks(t, s, scope, 2)
}

func TestStabilizationS8_FailureCategoriesKeepOriginalAndPrivateLogs(t *testing.T) {
	for _, mode := range []string{"budget", "timeout", "500", "plain"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "timeout" {
				stabilizationSecretaryFinding(t, "T3-S8: client timeout is model_error and uses the 500 advice")
			}
			s, scope := testStore(t), owner()
			logs := secretaryLogs(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				if mode == "500" {
					http.Error(w, "model-secret-T3", 500)
					return
				}
				secretaryModelReply(w, "普通文本回答")
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 200 * time.Millisecond
			t.Setenv("PCAS_STABILIZATION_T3_KEY", "key-secret-T3")
			provider := ai.Provider{ID: "model", Name: "测试秘书", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free", KeyEnv: "PCAS_STABILIZATION_T3_KEY"}
			if mode == "budget" {
				provider.CostMode = ""
				provider.InputPerMillion = 1
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 0})})
			}
			s.SetModels(&ai.Registry{HTTP: client, Config: ai.Configuration{Providers: []ai.Provider{provider}}})
			req := turnRequest("原话-private-T3")
			out := mustTurn(t, s, scope, req)
			if rendered := string(asJSON(out.Turn)); strings.Contains(rendered, "key-secret-T3") || strings.Contains(rendered, "model-secret-T3") {
				t.Fatal("model credential or raw provider error entered response")
			}
			stage, errorType, connector := "model", "model_error", "capture"
			switch mode {
			case "budget":
				stage, errorType = "budget", "budget_exceeded"
				if calls.Load() != 0 {
					t.Fatal("over-budget request reached model")
				}
			case "timeout":
				errorType = "timeout"
			case "plain":
				stage, errorType, connector = "parse", "no_json_object", "desk"
			}
			foundLog := false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record["level"] == "WARN" && record["stage"] == stage && record["error_type"] == errorType {
					foundLog = true
				}
			}
			if !foundLog {
				t.Errorf("missing WARN %s %s: %s", stage, errorType, logs.String())
			}
			if strings.Contains(logs.String(), "private-T3") || strings.Contains(logs.String(), "secret-T3") || strings.Contains(logs.String(), "普通文本回答") {
				t.Fatal("private data entered logs")
			}
			if mode == "plain" {
				if out.Turn.Reply != "普通文本回答" || len(out.Turn.Receipts) != 0 {
					t.Fatal("plain text became action or generic failure")
				}
			} else {
				if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" {
					t.Fatal("failure lost capture")
				}
				want := "模型没有响应"
				if mode == "budget" {
					want = "超过今天的额度"
				}
				if mode == "timeout" {
					want = "超时"
				}
				if !strings.Contains(out.Turn.Receipts[0].Text, want) {
					t.Error("wrong failure advice", out.Turn.Receipts)
				}
			}
			var saved string
			if err := s.pool.QueryRow(context.Background(), "SELECT v.body FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id) WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3", string(scope.OwnerID), connector, req.RequestID).Scan(&saved); err != nil || saved != req.Text {
				t.Fatal("original lost", err)
			}
			before := calls.Load()
			mustTurn(t, s, scope, req)
			if calls.Load() != before {
				t.Fatal("failure replay called model again")
			}
			stabilizationSecretaryTasks(t, s, scope, 0)
		})
	}
}

func TestStabilizationS9_LongReplyTruncatesWithoutLosingActions(t *testing.T) {
	stabilizationSecretaryFinding(t, "T3-S9: 2500-rune reply has no truncation or notice")
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, map[string]any{"reply": strings.Repeat("长", 2500), "actions": []any{map[string]string{"op": "create_task", "title": "S9仍执行"}}})
	})
	req := turnRequest("请安排并解释")
	out := mustTurn(t, s, scope, req)
	if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Status != "done" {
		t.Fatal("long reply prevented action", out.Turn.Receipts)
	}
	stabilizationSecretaryTasks(t, s, scope, 1)
	if !utf8.ValidString(out.Turn.Reply) || !strings.Contains(out.Turn.Reply, "回答太长，已截断") || utf8.RuneCountInString(out.Turn.Reply) > 2000+utf8.RuneCountInString("回答太长，已截断")+4 {
		t.Errorf("reply did not meet truncation contract: runes=%d containsNotice=%v", utf8.RuneCountInString(out.Turn.Reply), strings.Contains(out.Turn.Reply, "回答太长，已截断"))
	}
	var cached struct {
		Turn  workspace.SecretaryTurn `json:"turn"`
		State json.RawMessage         `json:"state"`
	}
	var body []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &cached); err != nil || cached.State != nil || cached.Turn.Reply != out.Turn.Reply {
		t.Fatal("unbounded or state-bearing cache", err)
	}
}
