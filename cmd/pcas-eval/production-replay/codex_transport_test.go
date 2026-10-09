package main

import (
	"context"
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/ai"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexAdapterRecordsAndReplaysExactReplyAndUsage(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python transport is unavailable")
	}
	root := t.TempDir()
	fake := filepath.Join(root, "fictional-provider.py")
	// A small protocol fixture. It has no model, credentials, or network client.
	script := `#!/usr/bin/env python3
import json,sys

def emit(v):
 print(json.dumps(v),flush=True)
for line in sys.stdin:
 r=json.loads(line);m=r.get('method');i=r.get('id');p=r.get('params',{})
 if i is None:continue
 if m=='account/read':result={'account':{'type':'chatgpt'}}
 elif m=='thread/start':result={'thread':{'id':'fictional-thread'}}
 elif m=='turn/start':result={'turn':{'id':'fictional-turn'}}
 else:result={}
 emit({'id':i,'result':result})
 if m=='turn/start':
  emit({'method':'thread/tokenUsage/updated','params':{'threadId':p['threadId'],'turnId':'fictional-turn','tokenUsage':{'total':{'inputTokens':123,'outputTokens':45}}}})
  emit({'method':'item/completed','params':{'threadId':p['threadId'],'turnId':'fictional-turn','item':{'type':'agentMessage','text':'{"answer":"fictional"}'}}})
  emit({'method':'turn/completed','params':{'threadId':p['threadId'],'turn':{'id':'fictional-turn','status':'completed'}}})
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	shim, err := filepath.Abs("../../../scripts/foundation_model_replay.py")
	if err != nil {
		t.Fatal(err)
	}
	cassette := filepath.Join(root, "codex.jsonl")
	var results []ai.Result
	for _, mode := range []string{"record", "replay"} {
		home := filepath.Join(root, mode)
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		cfg := map[string]any{"mode": mode, "case_id": "fictional", "record_file": cassette, "state_file": filepath.Join(home, "state.json"), "call_limit": 1}
		if mode == "record" {
			cfg["real_binary"] = fake
		} else {
			if err := os.Remove(fake); err != nil {
				t.Fatal(err)
			}
		}
		if err := writePrivate(filepath.Join(home, ".pcas-replay.json"), cfg); err != nil {
			t.Fatal(err)
		}
		c, err := ai.NewCodex(shim, home)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		registry := &ai.Registry{Codex: c, Config: ai.Configuration{Providers: []ai.Provider{{ID: "fictional", Name: "Fictional", Protocol: "codex", Model: "fictional", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		got, err := registry.GenerateSchema(ctx, "fictional", "Fictional instruction", "Fictional input", json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`))
		cancel()
		c.Close()
		if err != nil {
			t.Fatal(mode, err)
		}
		results = append(results, got)
		var state captureState
		if err := readPrivate(filepath.Join(home, "state.json"), &state); err != nil {
			t.Fatal(err)
		}
		if state.Completed != 1 || len(state.Errors) != 0 || mode == "replay" && state.LiveProviderStarts != 0 {
			t.Fatal(mode, state)
		}
	}
	if results[0].Text != results[1].Text || results[1].InputTokens != 123 || results[1].OutputTokens != 45 || results[0].Cost != results[1].Cost {
		t.Fatal("adapter reply or usage differs", results)
	}
}
