package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase2ContextHTTPFinalBytesAndBeforeDispatchRefusal(t *testing.T) {
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		for _, refuse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refuse_%t", protocol, refuse), func(t *testing.T) {
				var mu sync.Mutex
				var received [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					received = append(received, body)
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					switch protocol {
					case "openai":
						fmt.Fprint(w, `{"choices":[{"message":{"content":"合成观察回执"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
					case "responses":
						fmt.Fprint(w, `{"output":[{"content":[{"type":"output_text","text":"合成观察回执"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`)
					case "anthropic":
						fmt.Fprint(w, `{"content":[{"type":"text","text":"合成观察回执"}],"usage":{"input_tokens":2,"output_tokens":1}}`)
					}
				}))
				defer server.Close()
				var events []memory.ContextRequestEvent
				r := &Registry{HTTP: server.Client(), Config: Configuration{Providers: []Provider{{ID: "phase2-http", Protocol: protocol, BaseURL: server.URL + "/v1", Model: "phase2-selected", MaxOutput: 100}}}}
				r.ContextObserver = memory.ContextRequestObserverFunc(func(_ context.Context, event memory.ContextRequestEvent) error {
					event.Payload = append([]byte(nil), event.Payload...)
					events = append(events, event)
					if refuse && event.Stage == "before_dispatch" {
						return errors.New("phase2_fence_refused")
					}
					return nil
				})
				out, err := r.Generate(context.Background(), "phase2-http", "合成固定说明", "中文\n引号\"与反斜线\\及<&>。")
				mu.Lock()
				actual := append([][]byte(nil), received...)
				mu.Unlock()
				wantStages := []string{"prepared", "before_dispatch", "dispatched"}
				if refuse {
					wantStages = wantStages[:2]
					if err == nil || len(actual) != 0 || out.Text != "" {
						t.Error("before_dispatch refusal still sent actual HTTP or produced a successful answer")
					}
				} else if err != nil || out.Text != "合成观察回执" || len(actual) != 1 {
					t.Fatalf("actual adapter failed: requests=%d result=%+v error=%v", len(actual), out, err)
				}
				var stages []string
				for _, event := range events {
					stages = append(stages, event.Stage)
					if event.ObservationLayer != "serialized_request" || event.ProviderID != "phase2-http" || event.Protocol != protocol || event.Model != "phase2-selected" || event.Endpoint != server.URL+"/v1" {
						t.Error("final observation lost actual recipient or transport layer")
					}
					if len(actual) == 1 && !bytes.Equal(event.Payload, actual[0]) {
						t.Error("observed final bytes differ from actual HTTP receive body")
					}
					var payload struct {
						Model string `json:"model"`
					}
					if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.Model != event.Model {
						t.Error("observed model disagrees with actual serialized model", err)
					}
				}
				if !reflect.DeepEqual(stages, wantStages) {
					t.Errorf("observed stages=%v want=%v", stages, wantStages)
				}
				body, _ := json.Marshal(map[string]any{"protocol": protocol, "refused": refuse, "events": events, "actual_http_bodies": actual})
				t.Logf("phase2-http-observation=%s", body)
			})
		}
	}
}

func TestPhase2ContextCodexActualArgumentsAndBeforeTurnRefusal(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("synthetic app-server fixture requires Python; no skip", err)
	}
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse_%t", refuse), func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "actual-rpc.jsonl")
			captureJSON, _ := json.Marshal(capture)
			path := filepath.Join(dir, "fake-codex")
			// This is the existing fake app-server protocol pattern. The production
			// Codex client performs all parsing; the fake only records/answers RPCs.
			script := "#!" + python + "\n" + `import sys,json
capture=` + string(captureJSON) + `
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 with open(capture,'a') as f: f.write(json.dumps(m)+'\n')
 method=m.get('method');out={}
 if method=='account/read':out={'account':{'type':'chatgpt','email':'phase2@example.invalid','planType':'plus'}}
 if method=='thread/start':out={'thread':{'id':'phase2-thread'}}
 if method=='turn/start':out={'turn':{'id':'phase2-turn'}}
 print(json.dumps({'id':m['id'],'result':out}),flush=True)
 if method=='turn/start':
  print(json.dumps({'method':'item/completed','params':{'threadId':'phase2-thread','item':{'type':'agentMessage','text':'synthetic-codex'}}}),flush=True)
  print(json.dumps({'method':'turn/completed','params':{'threadId':'phase2-thread','turn':{'status':'completed'}}}),flush=True)
`
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			codex, err := NewCodex(path, filepath.Join(dir, "auth"))
			if err != nil {
				t.Fatal(err)
			}
			defer codex.Close()
			r := &Registry{Codex: codex, Config: Configuration{Providers: []Provider{{ID: "phase2-codex", Protocol: "codex", Model: "phase2-selected"}}}}
			var events []memory.ContextRequestEvent
			r.ContextObserver = memory.ContextRequestObserverFunc(func(_ context.Context, event memory.ContextRequestEvent) error {
				event.Payload = append([]byte(nil), event.Payload...)
				events = append(events, event)
				if refuse && event.Stage == "before_dispatch" {
					return errors.New("phase2_fence_refused")
				}
				return nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := r.Generate(ctx, "phase2-codex", "fixed synthetic instructions", "中文\\\"turn-only-controlled-payload")
			if refuse && (err == nil || out.Text != "") || !refuse && (err != nil || out.Text != "synthetic-codex") {
				t.Fatalf("synthetic actual Codex result=%+v error=%v refuse=%t", out, err, refuse)
			}
			raw, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var thread, turn json.RawMessage
			turnCalls := 0
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
				var message struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if err := json.Unmarshal(line, &message); err != nil {
					t.Fatal(err)
				}
				if message.Method == "thread/start" {
					thread = message.Params
				}
				if message.Method == "turn/start" {
					turn, turnCalls = message.Params, turnCalls+1
				}
			}
			wantStages := []string{"prepared", "before_dispatch", "dispatched"}
			if refuse {
				wantStages = wantStages[:2]
				if turnCalls != 0 {
					t.Error("before_dispatch refusal still sent actual Codex controlled turn arguments")
				}
			} else if turnCalls != 1 {
				t.Error("actual Codex turn not observed exactly once")
			}
			var stages []string
			for _, event := range events {
				stages = append(stages, event.Stage)
				if event.ObservationLayer != "adapter_arguments" || event.Protocol != "codex" || event.ProviderID != "phase2-codex" || event.Model != "phase2-selected" {
					t.Error("Codex observation falsely claims serialized HTTP or loses model/recipient")
				}
				var observed map[string]any
				if err := json.Unmarshal(event.Payload, &observed); err != nil {
					t.Fatal(err)
				}
				var actualThread, actualTurn any
				if err := json.Unmarshal(thread, &actualThread); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(observed["thread_start"], actualThread) {
					t.Error("observed thread arguments differ from actual app-server RPC")
				}
				if !refuse {
					if err := json.Unmarshal(turn, &actualTurn); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(observed["turn_start"], actualTurn) {
						t.Error("observed controlled turn arguments differ from actual app-server RPC")
					}
				}
			}
			if !reflect.DeepEqual(stages, wantStages) {
				t.Errorf("Codex stages=%v want=%v", stages, wantStages)
			}
			body, _ := json.Marshal(map[string]any{"refused": refuse, "events": events, "actual_app_server_rpc": string(raw), "actual_turn_calls": turnCalls, "remote_network_serialization": "unknown", "fixed_instruction_thread_created_before_barrier": true})
			t.Logf("phase2-codex-observation=%s", body)
		})
	}
}
