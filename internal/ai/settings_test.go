package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestManagedConnectionsSharedWithWorker(t *testing.T) {
	var textCalls, vectorCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch req.URL.Path {
		case "/v1/chat/completions":
			textCalls++
			if req.Header.Get("Authorization") != "Bearer text-secret" || body["model"] != "gpt-6.1-sol" {
				t.Error("wrong text credentials or model")
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
		case "/v1/embeddings":
			vectorCalls++
			if req.Header.Get("Authorization") != "Bearer vector-secret" || body["model"] != "text-embedding-3-small" || body["encoding_format"] != "float" {
				t.Error("wrong embedding request")
			}
			fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]}]}`)
		default:
			t.Error("unexpected path", req.URL.Path)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "private", "model-api.json")
	r := &Registry{SettingsPath: path, HTTP: server.Client()}
	worker := &Registry{SettingsPath: path, HTTP: server.Client()}
	text := Connection{BaseURL: server.URL + "/v1", Model: "gpt-6.1-sol", APIKey: "text-secret", InputPrice: 1, OutputPrice: 4, Default: true}
	if err := r.SaveConnection("text", text); err != nil {
		t.Fatal(err)
	}
	if worker.ExtractionID() != "openai-api" || !worker.Available("openai-api") {
		t.Fatal("worker did not load settings")
	}
	if result, err := worker.Generate(context.Background(), worker.ExtractionID(), "system", "prompt"); err != nil || result.Text != "OK" {
		t.Fatal(result, err)
	}
	vector := Connection{BaseURL: server.URL + "/v1", Model: "text-embedding-3-small", APIKey: "vector-secret", InputPrice: 0.2}
	if err := r.SaveConnection("embedding", vector); err != nil {
		t.Fatal(err)
	}
	if vectors, err := worker.EmbedQuery(context.Background(), "query"); err != nil || len(vectors) != 1 || vectors[0].Model != "openai-embedding:text-embedding-3-small" {
		t.Fatal(vectors, err)
	}
	if textCalls != 1 || vectorCalls != 1 {
		t.Fatal("missing provider calls")
	}
	status, _ := json.Marshal(r.ConnectionStatus("text"))
	if strings.Contains(string(status), "secret") || strings.Contains(string(status), "api_key") {
		t.Fatal("credential leaked")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials not private", err)
	}
	text.APIKey = ""
	text.Model = "alternate"
	if err := r.SaveConnection("text", text); err != nil {
		t.Fatal("blank key did not preserve same endpoint", err)
	}
	p, _ := worker.Get("openai-api")
	if p.apiKey != "text-secret" || p.Model != "alternate" {
		t.Fatal("key or model not preserved")
	}
	text.BaseURL = "https://other.invalid/v1"
	if err := r.SaveConnection("text", text); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("reused credential on different endpoint", err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if worker.Available("openai-api") {
		t.Fatal("corrupt file silently enabled provider")
	}
}

func TestManagedConnectionsAtomicReads(t *testing.T) {
	r := &Registry{SettingsPath: filepath.Join(t.TempDir(), "api.json")}
	c := Connection{BaseURL: "https://api.openai.com/v1", Model: "test", APIKey: "secret", InputPrice: 1}
	if err := r.SaveConnection("text", c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := r.SaveConnection("text", c); err != nil {
					t.Error(err)
				}
				if !r.Available("openai-api") {
					t.Error("torn settings read")
				}
			}
		}()
	}
	wg.Wait()
}

func TestSubscriptionDefaultPinned(t *testing.T) {
	r, err := Load("", &Codex{})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := r.Get("chatgpt")
	if !ok || p.Model != "gpt-6.1-sol" {
		t.Fatal("subscription still inherits Codex model")
	}
}
