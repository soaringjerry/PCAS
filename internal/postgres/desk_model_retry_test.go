package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryModelRetryClassification(t *testing.T) {
	for _, category := range []string{"rateLimitExceeded", "flexUnavailable", "serverOverloaded", "internalServerError", "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts"} {
		for _, status := range []int{0, 408, 429, 500, 502, 503, 504} {
			if !retryableSecretaryModelError(fmt.Errorf("wrapped: %w", &ai.CodexError{Category: category, HTTPStatus: status})) {
				t.Errorf("did not retry %s HTTP %d", category, status)
			}
		}
		for _, status := range []int{400, 401, 403, 404, 501, 505} {
			if retryableSecretaryModelError(&ai.CodexError{Category: category, HTTPStatus: status}) {
				t.Errorf("retried %s HTTP %d", category, status)
			}
		}
	}
	for _, category := range []string{"contextWindowExceeded", "sessionBudgetExceeded", "usageLimitExceeded", "cyberPolicy", "misalignmentPolicyViolation", "tooManyDenials", "unauthorized", "badRequest", "threadRollbackFailed", "sandboxError", "other", "activeTurnNotSteerable", "unknown", "refresh_token_reused", "refresh_token_expired", "refresh_token_invalidated", "sign_in_required", "transport_error", "event_buffer_exceeded", "turn_failed", "turn_interrupted", "empty_output", "invalid_thread", "invalid_turn"} {
		if retryableSecretaryModelError(&ai.CodexError{Category: category}) {
			t.Errorf("retried terminal category %s", category)
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("unclassified"), &ai.CodexError{Category: "serverOverloaded", Cause: context.Canceled}} {
		if retryableSecretaryModelError(err) {
			t.Errorf("retried cancellation/unknown error %v", err)
		}
	}
}

func TestSecretaryModelRetrySharesDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{100 * time.Millisecond, 800 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			deadline, _ := ctx.Deadline()
			calls := 0
			start := time.Now()
			_, err := retrySecretaryModel(ctx, func(callCtx context.Context) (ai.Result, error) {
				calls++
				if got, ok := callCtx.Deadline(); !ok || !got.Equal(deadline) {
					t.Fatal("attempt extended the deadline")
				}
				if calls > 1 {
					<-callCtx.Done()
				}
				return ai.Result{}, &ai.CodexError{Category: "serverOverloaded"}
			})
			wantCalls := 1
			if timeout > secretaryModelRetryDelay {
				wantCalls = 2
			}
			if !errors.Is(err, context.DeadlineExceeded) || calls != wantCalls || time.Since(start) > timeout+time.Second {
				t.Fatal("deadline was not respected", calls, err, time.Since(start))
			}
		})
	}
}

// The fake CLI fails real app-server turns, rather than mocking DeskTurn. Its
// state also verifies that both attempts get exactly the same model context.
func secretaryRetryCodex(t *testing.T, s *Store, failures, gate int, category string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python protocol helper unavailable")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-codex")
	config, _ := json.Marshal(map[string]any{"failures": failures, "gate": gate, "category": category, "dir": dir})
	script := `#!/usr/bin/env python3
import json,sys,pathlib,time
config=json.loads(` + strconv.Quote(string(config)) + `)
directory=pathlib.Path(config['dir']); calls=[]; threads={}; sequence=0
def emit(x): print(json.dumps(x),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method'); p=m.get('params',{}); out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start':
  sequence+=1; thread='thread-'+str(sequence); threads[thread]=p
  out={'thread':{'id':thread}}
 if method=='turn/start':
  thread=p['threadId']; turn='turn-'+str(len(calls)+1)
  calls.append({'thread':threads[thread],'input':p['input'],'schema':p['outputSchema']})
  (directory/'calls.json').write_text(json.dumps(calls))
  out={'turn':{'id':turn}}
 emit({'id':m['id'],'result':out})
 if method=='turn/start':
  if config['gate']==len(calls):
   (directory/'started').write_text('yes')
   while not (directory/'release').exists(): time.sleep(.01)
  if config['failures']<0 or len(calls)<=config['failures']:
   error={'codexErrorInfo':config['category'],'message':'model-private secret-token','additionalDetails':'private-prompt'}
   emit({'method':'turn/completed','params':{'threadId':thread,'turn':{'id':turn,'status':'failed','error':error}}})
  else:
   text=json.dumps({'reply':'安排好了。','used':[],'links':[],'show':[],'remember':False,'actions':[{'op':'create_task','title':'Q5虚构事项'}],'ask':None},ensure_ascii=False)
   emit({'method':'item/completed','params':{'threadId':thread,'item':{'type':'agentMessage','text':text}}})
   emit({'method':'turn/completed','params':{'threadId':thread,'turn':{'id':turn,'status':'completed'}}})
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := ai.NewCodex(binary, filepath.Join(dir, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	models, err := ai.Load("", c)
	if err != nil {
		t.Fatal(err)
	}
	s.SetModels(models)
	return dir
}

func secretaryRetryCalls(t *testing.T, dir string) []json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []json.RawMessage
	if err := json.Unmarshal(data, &calls); err != nil {
		t.Fatal(err)
	}
	return calls
}

func TestSecretaryModelRetryOutcomeAndIdempotency(t *testing.T) {
	for _, tt := range []struct {
		name, category  string
		failures, calls int
		success         bool
	}{
		{"recovers", "serverOverloaded", 1, 2, true},
		{"exhausted", "rateLimitExceeded", -1, 2, false},
		{"login expired", "unauthorized", 1, 1, false},
		{"content refused", "cyberPolicy", 1, 1, false},
		{"unknown", "other", 1, 1, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, scope := testStore(t), owner()
			dir := secretaryRetryCodex(t, s, tt.failures, 0, tt.category)
			logs := secretaryLogs(t)
			req := turnRequest("建一条Q5虚构事项 user-private")
			req.AgentID = "chatgpt"
			out := mustTurn(t, s, scope, req)
			calls := secretaryRetryCalls(t, dir)
			if len(calls) != tt.calls {
				t.Fatal("wrong attempt count", len(calls))
			}
			if len(calls) == 2 && !bytes.Equal(calls[0], calls[1]) {
				t.Fatal("retry changed instructions, context or schema")
			}
			connector, tasks, actions, usage := "capture", 0, 0, 0
			if tt.success {
				connector, tasks, actions, usage = "desk", 1, 1, 1
				if out.Turn.Reply != "安排好了。" || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "create_task" || strings.Contains(logs.String(), "secretary capture fallback") {
					t.Fatal("intermediate failure reached the user", out.Turn)
				}
			} else if len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" || out.Turn.Receipts[0].Text != "已记下原话；模型没有响应，稍后会自动整理" {
				t.Fatal("changed final fallback", out.Turn)
			}
			assertRows := func() {
				t.Helper()
				for _, check := range []struct {
					sql   string
					count int
				}{
					{"SELECT count(*) FROM work_items WHERE owner_id=$1 AND kind='task'", tasks},
					{"SELECT count(*) FROM action_log WHERE owner_id=$1", actions},
					{"SELECT count(*) FROM desk_turns WHERE owner_id=$1", 1},
					{"SELECT count(*) FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id) WHERE s.owner_id=$1 AND s.connector='" + connector + "'", 1},
					{"SELECT count(*) FROM background_usage WHERE owner_id=$1", tt.calls},
					{"SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='secretary'", usage},
				} {
					var count int
					if err := s.pool.QueryRow(context.Background(), check.sql, string(scope.OwnerID)).Scan(&count); err != nil || count != check.count {
						t.Fatal(check.sql, count, check.count, err)
					}
				}
				var cost float64
				if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil || cost != 0 {
					t.Fatal("failed subscription attempts left reserved spending", cost, err)
				}
			}
			assertRows()
			var original string
			if err := s.pool.QueryRow(context.Background(), "SELECT v.body FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id) WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3", string(scope.OwnerID), connector, req.RequestID).Scan(&original); err != nil || original != req.Text {
				t.Fatal("original was not saved exactly once", err)
			}
			if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "secret-token") {
				t.Fatal("private data in retry logs")
			}
			replay := mustTurn(t, s, scope, req)
			if !reflect.DeepEqual(out.Turn, replay.Turn) || len(secretaryRetryCalls(t, dir)) != tt.calls {
				t.Fatal("request replay repeated a model call or changed the result")
			}
			assertRows()
		})
	}
}

func TestSecretaryModelRetryDefersBusinessWrites(t *testing.T) {
	s, scope := testStore(t), owner()
	dir := secretaryRetryCodex(t, s, 1, 2, "serverOverloaded")
	req := turnRequest("建一条Q5虚构事项")
	req.AgentID = "chatgpt"
	done := make(chan error, 1)
	go func() { _, err := s.DeskTurn(context.Background(), scope, req); done <- err }()
	secretaryRetryGate(t, dir, done, func() {
		for _, table := range []string{"work_items", "sources", "action_log"} {
			var count int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 0 {
				t.Error("business write before final model result", table, count, err)
			}
		}
	})
}

func TestSecretaryModelRetryPreservesReplyAfterUnrelatedTaskChange(t *testing.T) {
	s, scope := testStore(t), owner()
	dir := secretaryRetryCodex(t, s, 1, 1, "serverOverloaded")
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Q5原名称"})
	req := turnRequest("修改Q5事项")
	req.AgentID = "chatgpt"
	done := make(chan error, 1)
	go func() { _, err := s.DeskTurn(context.Background(), scope, req); done <- err }()
	secretaryRetryGate(t, dir, done, func() {
		workspaceCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: state.Tasks[0].ID, Title: "Q5后来的名称"})
	})
	if len(secretaryRetryCalls(t, dir)) != 2 {
		t.Fatal("unrelated task change prevented reply retry")
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Tasks) != 2 {
		t.Fatal(state, err)
	}
	titles := map[string]bool{}
	for _, task := range state.Tasks {
		titles[task.Title] = true
	}
	if !titles["Q5后来的名称"] || !titles["Q5虚构事项"] {
		t.Fatal("reply actions or concurrent edit lost", titles)
	}

}

func secretaryRetryGate(t *testing.T, dir string, done <-chan error, change func()) {
	t.Helper()
	release := func() { _ = os.WriteFile(filepath.Join(dir, "release"), nil, 0600) }
	defer release()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake call did not reach gate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	change()
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("secretary did not finish after releasing fake call")
	}
}
