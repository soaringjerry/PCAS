package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPProtocolsAndEmbeddingOrder(t *testing.T) {
	t.Setenv("PCAS_TEST_MODEL_KEY", "secret-test-key")
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if protocol == "anthropic" {
					if r.Header.Get("x-api-key") != "secret-test-key" {
						t.Error("missing key")
					}
				} else if r.Header.Get("Authorization") != "Bearer secret-test-key" {
					t.Error("missing bearer")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "test-model" {
					t.Error("model not configured")
				}
				w.Header().Set("Content-Type", "application/json")
				switch protocol {
				case "openai":
					if r.URL.Path != "/v1/chat/completions" {
						t.Error(r.URL.Path)
					}
					fmt.Fprint(w, `{"choices":[{"message":{"content":"真实适配测试"}}],"usage":{"prompt_tokens":20,"completion_tokens":10}}`)
				case "responses":
					if body["store"] != false || r.URL.Path != "/v1/responses" {
						t.Error("responses request")
					}
					fmt.Fprint(w, `{"output":[{"content":[{"type":"output_text","text":"真实适配测试"}]}],"usage":{"input_tokens":20,"output_tokens":10}}`)
				case "anthropic":
					if r.URL.Path != "/v1/messages" {
						t.Error(r.URL.Path)
					}
					fmt.Fprint(w, `{"content":[{"type":"text","text":"真实适配测试"}],"usage":{"input_tokens":20,"output_tokens":10}}`)
				}
			}))
			defer server.Close()
			r := &Registry{HTTP: server.Client(), Config: Configuration{Providers: []Provider{{ID: "test", Protocol: protocol, BaseURL: server.URL + "/v1", Model: "test-model", KeyEnv: "PCAS_TEST_MODEL_KEY", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}}
			out, err := r.Generate(context.Background(), "test", "system", "prompt")
			if err != nil || out.Text != "真实适配测试" || out.Cost != 0.00004 {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)
	}))
	defer server.Close()
	r := &Registry{HTTP: server.Client(), Config: Configuration{Embedding: "embedding", Providers: []Provider{{ID: "embedding", Protocol: "openai", Model: "small", BaseURL: server.URL, Embedding: true}}}}
	v, err := r.Embed(context.Background(), []string{"first", "second"})
	if err != nil || len(v) != 2 || v[0].Values[0] != 1 || v[1].Values[1] != 1 {
		t.Fatal("embedding ordering", v, err)
	}
}
func TestProviderErrorDoesNotLeakResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "SECRET_PROVIDER_RESPONSE")
	}))
	defer server.Close()
	r := &Registry{HTTP: server.Client(), Config: Configuration{Providers: []Provider{{ID: "test", Protocol: "openai", BaseURL: server.URL}}}}
	_, err := r.Generate(context.Background(), "test", "s", "p")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}
func TestCodexManagedProtocol(t *testing.T) {
	python, err := os.Stat("/usr/bin/python3")
	if err != nil || python.IsDir() {
		t.Skip("Python helper unavailable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-codex")
	script := `#!/usr/bin/python3
import sys,json,os
assert 'PCAS_TEST_SECRET' not in os.environ
def emit(x):
 print(json.dumps(x),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method');p=m.get('params',{});out={}
 if method=='account/read': out={'account':{'type':'chatgpt','email':'test@example.invalid','planType':'plus'}}
 if method=='account/login/start':
  assert p['type']=='chatgptDeviceCode'
  out={'type':'chatgptDeviceCode','loginId':'login','verificationUrl':'https://auth.openai.com/codex/device','userCode':'TEST-123'}
 if method=='thread/start':
  assert p['ephemeral'] and p['sandbox']=='read-only' and p['approvalPolicy']=='never'
  out={'thread':{'id':'thread-1'}}
 if method=='turn/start': out={'turn':{'id':'turn-1'}}
 emit({'id':m['id'],'result':out})
 if method=='turn/start':
  emit({'method':'item/completed','params':{'threadId':'unrelated','item':{'type':'agentMessage','text':'SHOULD_NOT_LEAK'}}})
  emit({'method':'item/completed','params':{'threadId':'thread-1','item':{'type':'agentMessage','text':'订阅结果'}}})
  emit({'method':'turn/completed','params':{'threadId':'thread-1','turn':{'status':'completed'}}})
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCAS_TEST_SECRET", "private")
	c, err := NewCodex(path, filepath.Join(dir, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	login, err := c.Login(ctx)
	if err != nil || !strings.Contains(string(login), "TEST-123") {
		t.Fatal(string(login), err)
	}
	result, err := c.Generate(ctx, "", "system", "prompt")
	if err != nil || result != "订阅结果" {
		t.Fatal(result, err)
	}
}
func TestInstalledCodexHandshake(t *testing.T) {
	binary := os.Getenv("PCAS_TEST_CODEX_BINARY")
	if binary == "" {
		t.Skip("optional installed CLI protocol check")
	}
	c, err := NewCodex(binary, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := c.Account(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var account struct {
		Account any `json:"account"`
	}
	if json.Unmarshal(result, &account) != nil || account.Account != nil {
		t.Fatal("private profile must not inherit ambient login")
	}
}

func TestTranscriptionAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Error("wrong endpoint")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("model") != "speech-test" {
			t.Error("missing model")
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
		} else {
			f.Close()
			if h.Filename != "voice.wav" {
				t.Error("filename")
			}
		}
		_, _ = w.Write([]byte(`{"text":"原文转录"}`))
	}))
	defer server.Close()
	r := &Registry{HTTP: server.Client(), Config: Configuration{Transcription: "speech", Providers: []Provider{{ID: "speech", Protocol: "openai", Model: "speech-test", BaseURL: server.URL, Transcription: true}}}}
	text, err := r.Transcribe(context.Background(), strings.NewReader("test-only"), "voice.wav")
	if err != nil || text != "原文转录" {
		t.Fatal(text, err)
	}
}
