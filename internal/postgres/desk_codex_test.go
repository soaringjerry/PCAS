package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestCodexSecretaryAndLegacyFormats(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python protocol helper unavailable")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-codex")
	script := fmt.Sprintf(`#!/usr/bin/env python3
import sys,json
system=''
web=False
def emit(x): print(json.dumps(x),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m['method']; p=m.get('params',{}); out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start':
  system=p['baseInstructions']; web=p.get('config',{}).get('web_search')=='live'
  assert 'Follow the output format specified in the base instructions.' in p['developerInstructions']
  out={'thread':{'id':'thread'}}
 if method=='turn/start':
  prompt=p['input'][0]['text']
  if '前台秘书' in system:
   assert web and prompt.endswith('只输出 JSON 对象。\n')
   schema=p['outputSchema']; props=schema['properties']
   assert set(props)=={'reply','used','links','show','remember','actions','ask'}
   assert set(schema['required'])==set(props) and schema['additionalProperties']==False
   variants=props['actions']['items']['anyOf']
   assert {v['properties']['op']['enum'][0] for v in variants}=={'create_task','update','create_idea','create_project','add_steps','delegate'}
   value={'reply':'安排好了。','used':[],'links':[],'show':[],'remember':False,'actions':[{'op':'create_task','title':'给张三回邮件','due':'%s','remind':None,'project':None,'notes':None,'owedTo':None,'waitingFor':None}],'ask':None}
  elif '导办台' in system:
   assert web and 'outputSchema' not in p
   value={'answer':'没有相关记录。','used':[],'links':[]}
  else:
   assert not web and 'outputSchema' not in p
   value={'draft':'邮件草稿'} if 'JSON' in prompt else '邮件草稿：您好。'
  text=value if isinstance(value,str) else json.dumps(value,ensure_ascii=False)
  out={'turn':{'id':'turn'}}
 emit({'id':m['id'],'result':out})
 if method=='turn/start':
  emit({'method':'item/completed','params':{'threadId':'thread','item':{'type':'agentMessage','text':text}}})
  emit({'method':'turn/completed','params':{'threadId':'thread','turn':{'status':'completed'}}})
`, testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0).Format("2006-01-02T15:04"))
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	exerciseCodexFormats(t, binary, filepath.Join(dir, "home"), false)
}

// Explicit opt-in only: use a disposable Codex home with a developer's login
// and PCAS_TEST_DATABASE_URL pointing to a disposable database. All input is synthetic.
func TestLiveCodexSecretaryAndLegacyFormats(t *testing.T) {
	home := os.Getenv("PCAS_LIVE_CODEX_HOME")
	if home == "" {
		t.Skip("requires a dedicated PCAS_LIVE_CODEX_HOME")
	}
	exerciseCodexFormats(t, os.Getenv("PCAS_LIVE_CODEX_BINARY"), home, true)
}

func exerciseCodexFormats(t *testing.T, binary, home string, live bool) {
	t.Helper()
	s := testStore(t)
	scope := owner()
	c, err := ai.NewCodex(binary, home)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	models, err := ai.Load("", c)
	if err != nil {
		t.Fatal(err)
	}
	models.Config.Extraction = "chatgpt"
	s.SetModels(models)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai", "city": "上海"})})
	req := turnRequest("明天下午三点给张三回邮件")
	req.AgentID = "chatgpt"
	expected := testsupport.DateFromToday(t, "Asia/Shanghai", 1, 15, 0)
	if live {
		var prompt string
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			context, err := s.secretaryContextTx(ctx, tx, scope, req, string(memory.NewID()))
			if err != nil {
				return err
			}
			prompt, _, err = s.secretaryPrompt(ctx, tx, scope, req, &context)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		raw, err := models.GenerateWithSearchSchema(ctx, "chatgpt", secretaryInstructions, prompt, secretaryOutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parseSecretaryOutput(raw.Text)
		if !json.Valid([]byte(raw.Text)) || err != nil || len(parsed.Actions) != 1 || parsed.Actions[0].Op != "create_task" {
			t.Fatal("live output was not a JSON task")
		}
		t.Logf("raw secretary: valid_json=true create_task=1 seconds=%.1f", time.Since(start).Seconds())
	}
	start := time.Now()
	out, err := s.DeskTurn(ctx, scope, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.State.Tasks) != 1 || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "create_task" || out.Turn.Receipts[0].Status != "done" {
		t.Fatal("Codex task did not execute")
	}
	task := out.State.Tasks[0]
	due, err := time.Parse(time.RFC3339, task.Due)
	if err != nil || !due.Equal(expected) || !strings.Contains(task.Title, "张三") {
		t.Fatal("wrong task or local due time")
	}
	if len(task.Triggers) != 1 || task.Triggers[0].Offset != "-30m" || task.Triggers[0].NextAt != expected.Add(-30*time.Minute).UTC().Format(time.RFC3339) || !task.Triggers[0].Active {
		t.Fatal("missing default reminder")
	}
	t.Logf("DeskTurn: create_task=done due=%s reminder=%s seconds=%.1f", task.Due, task.Triggers[0].NextAt, time.Since(start).Seconds())
	if live {
		start = time.Now()
		weatherReq := turnRequest("今天天气怎么样")
		weatherReq.AgentID = "chatgpt"
		weather, err := s.DeskTurn(ctx, scope, weatherReq)
		if err != nil || strings.TrimSpace(weather.Turn.Reply) == "" || len(weather.Turn.Receipts) != 0 || strings.Contains(weather.Turn.Reply, "模型没有响应") || json.Valid([]byte(weather.Turn.Reply)) {
			t.Fatal("weather did not return a normal reply")
		}
		t.Logf("weather: reply_nonempty=true actions=0 capture=0 seconds=%.1f", time.Since(start).Seconds())
	}
	// Exercise the actual legacy HTTP route; it must keep its own answer JSON shape.
	token := strings.Repeat("f5-test-", 5)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	r := httptest.NewRequest(http.MethodPost, "/v1/desk/answer", strings.NewReader(string(asJSON(map[string]any{"agentId": "chatgpt", "question": "记录里有报销政策吗", "history": []workspace.DeskTurn{}}))))
	r = r.WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	var answer workspace.DeskAnswer
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &answer) != nil || strings.TrimSpace(answer.Answer) == "" {
		t.Fatal("legacy answer route failed", w.Code)
	}
	t.Log("legacy /v1/desk/answer: status=200 answer_nonempty=true")
	for _, format := range []string{"纯文本", "JSON 对象，包含 draft 字符串字段"} {
		st := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "chatgpt", Kind: "draft", Prompt: "起草一句礼貌邮件，只输出" + format})
		runID := st.Runs[0].ID
		if err := s.runAgentOnce(ctx); err != nil {
			t.Fatal(err)
		}
		st, err = s.Snapshot(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		var run workspace.Run
		for _, candidate := range st.Runs {
			if candidate.ID == runID {
				run = candidate
			}
		}
		if run.Status != "done" || run.Output == "" {
			t.Fatal("Codex run did not complete")
		}
		wantJSON := strings.HasPrefix(format, "JSON")
		if json.Valid([]byte(run.Output)) != wantJSON {
			t.Fatal("run output did not follow requested format")
		}
		t.Logf("assistant run: status=done json=%t", wantJSON)
	}
}
