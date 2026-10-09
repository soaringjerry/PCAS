package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestRequiredGenerationModesCannotFallBackToOrdinaryHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	registry := &Registry{HTTP: server.Client()}
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		p := Provider{ID: "selected", Protocol: protocol, BaseURL: server.URL}
		for _, mode := range []GenerationMode{{Search: true}, {Schema: json.RawMessage(`{"type":"object"}`)}, {Search: true, Schema: json.RawMessage(`{"type":"object"}`)}} {
			observed := 0
			ctx := WithInvocationObserver(context.Background(), func(int64) { observed++ })
			result, err := registry.GenerateProvider(ctx, p, "Fictitious instructions.", "Fictitious input.", mode)
			var capability *CapabilityError
			if !errors.Is(err, ErrUnsupportedCapability) || !errors.As(err, &capability) || capability.Capability == "" || result.DurationMS != nil || observed != 0 {
				t.Fatalf("protocol=%s result=%+v error=%v observations=%d", protocol, result, err, observed)
			}
		}
	}
	if calls != 0 {
		t.Fatalf("unsupported requests reached HTTP: %d", calls)
	}
}

func TestInvalidGenerationModesMakeNoInvocation(t *testing.T) {
	registry := &Registry{}
	provider := Provider{ID: "selected", Protocol: "openai"}
	for _, modes := range [][]GenerationMode{{{Schema: json.RawMessage(`[]`)}}, {{Schema: json.RawMessage(`null`)}}, {{Schema: json.RawMessage(`{"type":`)}}, {{}, {Search: true}}} {
		result, err := registry.GenerateProvider(context.Background(), provider, "Fictitious instructions.", "Fictitious input.", modes...)
		if !errors.Is(err, memory.ErrInvalid) || result.DurationMS != nil {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}

func TestUnavailableRequiredModeCannotStartCodex(t *testing.T) {
	registry := &Registry{}
	_, err := registry.GenerateProvider(context.Background(), Provider{Protocol: "codex"}, "Fictitious instructions.", "Fictitious input.", GenerationMode{Search: true})
	if !errors.Is(err, memory.ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestSelectedCodexModeKeepsModelSchemaOrderAndSearchIsolation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python protocol fixture required")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-fixture")
	script := `#!` + python + `
import json,sys
web=False
def emit(value): print(json.dumps(value),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 if 'id' not in m: continue
 method=m.get('method');p=m.get('params',{});out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start':
  assert p['model']=='selected-model'
  web=p.get('config')=={'web_search':'live'}
  assert web or 'config' not in p
  out={'thread':{'id':'thread'}}
 if method=='turn/start':
  name=p['input'][0]['text']
  assert web==(name=='search')
  if name!='ordinary':
   schema=p['outputSchema']
   assert list(schema['properties'])==['z','a']
   assert schema['required']==['z','a']
  else: assert 'outputSchema' not in p
  out={'turn':{'id':'turn'}}
 emit({'id':m['id'],'result':out})
 if method=='turn/start':
  if web: emit({'method':'item/completed','params':{'threadId':'thread','item':{'type':'webSearch','id':'search','query':'Fictitious public query.'}}})
  emit({'method':'item/completed','params':{'threadId':'thread','item':{'type':'agentMessage','text':'Fictitious reply.'}}})
  emit({'method':'turn/completed','params':{'threadId':'thread','turn':{'status':'completed'}}})
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	codex, err := NewCodex(path, filepath.Join(dir, "account"))
	if err != nil {
		t.Fatal(err)
	}
	defer codex.Close()
	registry := &Registry{Codex: codex, Config: Configuration{Providers: []Provider{{ID: "selected", Protocol: "codex", Model: "selected-model", MaxOutput: 100}}}}
	selected, _ := registry.Get("selected")
	registry.Config.Providers[0].Model = "replacement-model"
	schema := json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"}},"required":["z","a"],"additionalProperties":false}`)
	for _, test := range []struct {
		prompt string
		mode   GenerationMode
	}{{"search", GenerationMode{Search: true, Schema: schema}}, {"offline", GenerationMode{Schema: schema}}, {"ordinary", GenerationMode{}}} {
		result, err := registry.GenerateProvider(context.Background(), selected, "Fictitious instructions.", test.prompt, test.mode)
		if err != nil || result.Text != "Fictitious reply." || result.DurationMS == nil {
			t.Fatalf("mode=%s result=%+v error=%v", test.prompt, result, err)
		}
		if test.mode.Search && (len(result.Searches) != 1 || result.Searches[0] != "Fictitious public query.") || !test.mode.Search && len(result.Searches) != 0 {
			t.Fatalf("mode=%s search evidence=%v", test.prompt, result.Searches)
		}
	}
}
