package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCodexFailureDiagnostics(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python protocol helper unavailable")
	}
	for _, tt := range []struct {
		name, info, message, category string
		status, rpc                   int
		notificationOnly, recovers    bool
	}{
		{name: "overload", info: `"serverOverloaded"`, category: "serverOverloaded"},
		{name: "HTTP", info: `{"responseTooManyFailedAttempts":{"httpStatusCode":429}}`, category: "responseTooManyFailedAttempts", status: 429},
		{name: "refresh reuse", info: `"other"`, message: "Your refresh token has already been used private-token", category: "refresh_token_reused"},
		{name: "untrusted category", info: `"private-prompt"`, category: "unknown"},
		{name: "untrusted object", info: `{"private-token":{"httpStatusCode":429}}`, category: "unknown"},
		{name: "RPC", info: `"unauthorized"`, category: "unauthorized", rpc: -32000},
		{name: "notification fallback", info: `"rateLimitExceeded"`, category: "rateLimitExceeded", notificationOnly: true},
		{name: "provider retries", info: `"serverOverloaded"`, category: "serverOverloaded", recovers: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "fake-codex")
			wire, err := json.Marshal(map[string]any{
				"codexErrorInfo": json.RawMessage(tt.info), "message": "private-body " + tt.message,
				"additionalDetails": "private-credentials",
			})
			if err != nil {
				t.Fatal(err)
			}
			config, _ := json.Marshal(map[string]any{"error": json.RawMessage(wire), "rpc": tt.rpc, "notification": tt.notificationOnly, "recovers": tt.recovers})
			script := `#!/usr/bin/env python3
import sys,json
config=json.loads(` + strconv.Quote(string(config)) + `)
def emit(x): print(json.dumps(x),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method'); out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start': out={'thread':{'id':'thread'}}
 if method=='turn/start':
  if config['rpc']:
   emit({'id':m['id'],'error':{'code':config['rpc'],'message':config['error']['message'],'data':config['error']}})
   continue
  out={'turn':{'id':'turn'}}
 emit({'id':m['id'],'result':out})
 if method=='turn/start':
  emit({'method':'error','params':{'threadId':'unrelated','turnId':'turn','error':{'codexErrorInfo':'private-unrelated'},'willRetry':False}})
  emit({'method':'error','params':{'threadId':'thread','turnId':'turn','error':config['error'],'willRetry':config['recovers']}})
  turn={'id':'turn','status':'failed'}
  if not config['notification']: turn['error']=config['error']
  if config['recovers']:
   turn={'id':'turn','status':'completed'}
   emit({'method':'item/completed','params':{'threadId':'thread','item':{'type':'agentMessage','text':'success'}}})
  emit({'method':'turn/completed','params':{'threadId':'thread','turn':turn}})
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			prior := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(prior)
			c, err := NewCodex(binary, filepath.Join(dir, "home"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			text, err := c.Generate(ctx, "", "private-system", "private-prompt")
			if tt.recovers {
				if err != nil || text != "success" {
					t.Fatal("native retry did not finish", text, err)
				}
			} else {
				var detail *CodexError
				if !errors.As(err, &detail) || detail.Category != tt.category || detail.RPCCode != tt.rpc || detail.HTTPStatus != tt.status {
					t.Fatal("lost failure category", err)
				}
				if strings.Contains(err.Error(), "private") {
					t.Fatal("private error message escaped")
				}
			}
			if strings.Contains(logs.String(), "private") {
				t.Fatal("private data in logs", logs.String())
			}
			if logs.Len() == 0 {
				t.Fatal("failure not logged")
			}
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				for key := range record {
					switch key {
					case "time", "level", "msg", "operation", "category", "rpc_code", "http_status", "elapsed_ms", "pid", "provider_will_retry":
					default:
						t.Fatal("unexpected diagnostic field", key)
					}
				}
				if record["category"] != tt.category || record["operation"] == nil || record["elapsed_ms"] == nil || record["rpc_code"] == nil || record["http_status"] == nil {
					t.Fatal("missing diagnostic metadata", record)
				}
			}
		})
	}
}
