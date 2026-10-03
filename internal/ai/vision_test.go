package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V10: exact wire image payloads, using synthetic bytes and fake providers.
func TestVisionProtocolPayloads(t *testing.T) {
	data := []byte("synthetic image")
	encoded := base64.StdEncoding.EncodeToString(data)
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				var content []any
				switch protocol {
				case "responses":
					if r.URL.Path != "/responses" || body["store"] != false {
						t.Error("responses framing")
					}
					content = body["input"].([]any)[0].(map[string]any)["content"].([]any)
					if content[1].(map[string]any)["image_url"] != "data:image/png;base64,"+encoded {
						t.Error("Responses image")
					}
					fmt.Fprint(w, `{"output":[{"content":[{"type":"output_text","text":"notice"}]}],"usage":{"input_tokens":20,"output_tokens":10}}`)
				case "openai":
					if r.URL.Path != "/chat/completions" {
						t.Error(r.URL.Path)
					}
					content = body["messages"].([]any)[1].(map[string]any)["content"].([]any)
					if content[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/png;base64,"+encoded {
						t.Error("Chat image")
					}
					fmt.Fprint(w, `{"choices":[{"message":{"content":"notice"}}],"usage":{"prompt_tokens":20,"completion_tokens":10}}`)
				case "anthropic":
					if r.URL.Path != "/messages" {
						t.Error(r.URL.Path)
					}
					content = body["messages"].([]any)[0].(map[string]any)["content"].([]any)
					source := content[1].(map[string]any)["source"].(map[string]any)
					if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != encoded {
						t.Error("Anthropic image")
					}
					fmt.Fprint(w, `{"content":[{"type":"text","text":"notice"}],"usage":{"input_tokens":20,"output_tokens":10}}`)
				}
				if content[0].(map[string]any)["text"] != "read this" || body["model"] != "fake" {
					t.Error("instruction/model changed")
				}
			}))
			defer server.Close()
			r := &Registry{HTTP: server.Client(), Config: Configuration{Extraction: "vision", Providers: []Provider{{ID: "vision", Protocol: protocol, BaseURL: server.URL, Model: "fake", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}}
			got, err := r.Vision(context.Background(), "vision", "read this", Image{MediaType: "image/png", Data: data})
			if err != nil || got.Text != "notice" || got.InputTokens != 20 || got.OutputTokens != 10 || got.Cost != 0.00004 {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestVisionSelectionAndUnsupported(t *testing.T) {
	r := &Registry{Config: Configuration{Extraction: "text", Providers: []Provider{{ID: "text", Protocol: "manual"}, {ID: "embed", Protocol: "openai", Embedding: true}, {ID: "first", Protocol: "responses"}, {ID: "second", Protocol: "anthropic"}}}}
	if p, ok := r.VisionProvider(); !ok || p.ID != "first" {
		t.Fatal(p, ok)
	}
	r.Config.Extraction = "second"
	if p, ok := r.VisionProvider(); !ok || p.ID != "second" {
		t.Fatal(p, ok)
	}
	_, err := r.Vision(context.Background(), "text", "read", Image{MediaType: "image/png", Data: []byte("x")})
	if !errors.Is(err, ErrVisionUnsupported) || !strings.Contains(err.Error(), "这个通道不能看图") {
		t.Fatal(err)
	}
}

func TestVisionCodexRPC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-codex")
	script := `#!/usr/bin/python3
import sys,json
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method');p=m.get('params',{});out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start':
  assert p['ephemeral'] and p['sandbox']=='read-only' and p['approvalPolicy']=='never'
  out={'thread':{'id':'thread'}}
 if method=='turn/start':
  assert p['input']==[{'type':'text','text':'read this'},{'type':'image','url':'data:image/png;base64,c3ludGhldGljIGltYWdl'}]
  out={'turn':{'id':'turn'}}
 print(json.dumps({'id':m['id'],'result':out}),flush=True)
 if method=='turn/start':
  print(json.dumps({'method':'item/completed','params':{'threadId':'thread','item':{'type':'agentMessage','text':'notice'}}}),flush=True)
  print(json.dumps({'method':'turn/completed','params':{'threadId':'thread','turn':{'status':'completed'}}}),flush=True)
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := NewCodex(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := &Registry{Codex: c, Config: Configuration{Providers: []Provider{{ID: "c", Protocol: "codex", Model: "fake"}}}}
	out, err := r.Vision(context.Background(), "c", "read this", Image{MediaType: "image/png", Data: []byte("synthetic image")})
	if err != nil || out.Text != "notice" {
		t.Fatal(out, err)
	}
}
