package postgres_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
)

// Standard local transport, independent of compare/card/answer JSON formats.
// Later adapters encode only message.content; no real model/key is ever used.
type phase25B234ModelRequest struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}
type phase25B234Model struct {
	mu       sync.Mutex
	requests []phase25B234ModelRequest
}
type phase25B234ModelReply struct {
	status  int
	content string
}

func (m *phase25B234Model) calls() []phase25B234ModelRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]phase25B234ModelRequest{}, m.requests...)
}

func (f *phase25B234Fixture) model(t *testing.T, reply func(*http.Request, int, phase25B234ModelRequest) phase25B234ModelReply) *phase25B234Model {
	t.Helper()
	m := &phase25B234Model{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected fictitious model endpoint", http.StatusNotFound)
			return
		}
		var request phase25B234ModelRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&request); err != nil {
			http.Error(w, fmt.Sprintf("invalid fake model transport: %v", err), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.requests = append(m.requests, request)
		n := len(m.requests)
		m.mu.Unlock()
		// Do not hold the log mutex during callbacks: barriers and parallel
		// readers must be able to enter together. Do not Fatal in this goroutine.
		result := reply(r, n, request)
		status := result.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "fictitious channel failure"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": result.content}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 101, "completion_tokens": 37}})
	}))
	t.Cleanup(server.Close)
	t.Setenv("PCAS_P25_T234_FICTITIOUS_KEY", "fictitious-only-key")
	p := ai.Provider{ID: "phase25-b234-fake", Name: "虚构验收通道", Protocol: "openai", BaseURL: server.URL + "/v1", KeyEnv: "PCAS_P25_T234_FICTITIOUS_KEY", Model: "fictitious-model", CostMode: "free", MaxOutput: 4096}
	f.store.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: p.ID, Providers: []ai.Provider{p}}})
	return m
}
