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

// All organize output field names live here, independent of scenario assertions.
// Durable deliberately accepts any value so R9 tests can send invalid values.
type phase25B1ModelItem struct {
	Number       int      `json:"n"`
	Category     string   `json:"category"`
	Durable      any      `json:"durable"`
	Project      string   `json:"project,omitempty"`
	Topics       []string `json:"topics,omitempty"`
	Area         string   `json:"area,omitempty"`
	Deadlines    []any    `json:"deadlines"`
	Unrestricted bool     `json:"unrestricted"`
	Scope        string   `json:"scope"`
}

type phase25B1NewGroup struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"desc"`
}

func phase25B1ModelJSON(t *testing.T, items []phase25B1ModelItem, groups ...phase25B1NewGroup) string {
	t.Helper()
	for i := range items {
		items[i].Deadlines = []any{}
		items[i].Unrestricted = true
	}
	data, err := json.Marshal(struct {
		Items []phase25B1ModelItem `json:"items"`
		New   []phase25B1NewGroup  `json:"new,omitempty"`
	}{Items: items, New: groups})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type phase25B1ModelRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

type phase25B1ModelResponse struct {
	Text   string
	Status int // 0 means success; non-200 permits transient-channel scenarios.
}

type phase25B1FakeModel struct {
	registry *ai.Registry
	mu       sync.Mutex
	requests []phase25B1ModelRequest
}

// The callback runs synchronously inside the model request, without holding mu.
// It can correct/delete a memory, install a failing DB trigger, or coordinate
// concurrent workers using channels. It must return errors, never call t.Fatal
// from this HTTP goroutine. No model credentials or external endpoint are used.
func phase25B1NewFakeModel(t *testing.T, respond func(int, phase25B1ModelRequest) (phase25B1ModelResponse, error)) *phase25B1FakeModel {
	t.Helper()
	fake := &phase25B1FakeModel{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected fake model request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected fake endpoint", http.StatusBadRequest)
			return
		}
		var request phase25B1ModelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("fake model request JSON: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, request)
		call := len(fake.requests)
		fake.mu.Unlock()
		reply, err := respond(call, request)
		if err != nil {
			t.Errorf("fake model callback %d: %v", call, err)
			http.Error(w, "test callback failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if reply.Status != 0 && reply.Status != http.StatusOK {
			w.WriteHeader(reply.Status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "fictitious temporary model outage"}})
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id": fmt.Sprintf("fictitious-call-%d", call), "model": "fictitious-organize-model",
			"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": reply.Text}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 101, "completion_tokens": 37, "total_tokens": 138},
		}); err != nil {
			t.Errorf("fake response write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("PCAS_P25_B1_FAKE_MODEL_KEY", "fictitious-test-key")
	fake.registry = &ai.Registry{HTTP: server.Client(), Config: ai.Configuration{
		Extraction: "phase25-b1-fake",
		Providers: []ai.Provider{{ID: "phase25-b1-fake", Protocol: "openai", BaseURL: server.URL + "/v1",
			KeyEnv: "PCAS_P25_B1_FAKE_MODEL_KEY", Model: "fictitious-organize-model", CostMode: "free", MaxOutput: 4096}},
	}}
	return fake
}

func (f *phase25B1FakeModel) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func TestPhase25B1_FakeModelPreservesValidAndInvalidOutput(t *testing.T) {
	// Fixture verification only: this does not claim organizing acceptance.
	valid := phase25B1ModelJSON(t, []phase25B1ModelItem{
		{Number: 1, Category: "rule", Durable: false, Topics: []string{"虚构季度汇报"}, Area: "工作"},
		{Number: 1, Category: "goal", Durable: true}, // Keep duplicate n for R9.
		{Number: 999, Category: "event", Durable: "invalid-boolean"},
	}, phase25B1NewGroup{Type: "topic", Name: "虚构季度汇报", Description: "虚构汇报的准备"})
	responses := []string{valid, "deliberately invalid JSON"}
	fake := phase25B1NewFakeModel(t, func(call int, request phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		if call > len(responses) {
			return phase25B1ModelResponse{}, fmt.Errorf("unexpected extra call %d", call)
		}
		if request.Model != "fictitious-organize-model" || len(request.Messages) == 0 {
			return phase25B1ModelResponse{}, fmt.Errorf("unexpected model request")
		}
		return phase25B1ModelResponse{Text: responses[call-1]}, nil
	})
	for _, want := range responses {
		result, err := fake.registry.Generate(t.Context(), "phase25-b1-fake", "虚构验收模型", "虚构记忆一条。")
		if err != nil {
			t.Fatal(err)
		}
		if result.Text != want {
			t.Errorf("model fixture altered reply: got %q want %q", result.Text, want)
		}
		if result.InputTokens != 101 || result.OutputTokens != 37 {
			t.Errorf("fake usage = %d,%d", result.InputTokens, result.OutputTokens)
		}
	}
	if fake.calls() != len(responses) {
		t.Errorf("calls = %d", fake.calls())
	}
}
