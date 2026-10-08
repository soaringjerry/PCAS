package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSelectedProviderKeepsModelAndRatesAfterConfigurationChange(t *testing.T) {
	var calledModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calledModel = request.Model
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "Fictitious response."}}}, "usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2}})
	}))
	defer server.Close()
	registry := &Registry{HTTP: server.Client(), Config: Configuration{Providers: []Provider{{ID: "selected", Name: "Fictitious provider", Model: "selected-model", Protocol: "openai", BaseURL: server.URL, InputPerMillion: 1, OutputPerMillion: 2, MaxOutput: 100}}}}
	selected, ok := registry.Get("selected")
	if !ok {
		t.Fatal("provider unavailable")
	}
	registry.Config.Providers[0].Model = "replacement-model"
	registry.Config.Providers[0].InputPerMillion = 200
	result, err := registry.GenerateProvider(context.Background(), selected, "Fictitious instructions.", "Fictitious input.")
	if err != nil || calledModel != "selected-model" || result.Cost != 9.0/1e6 || result.InputTokens != 5 || result.OutputTokens != 2 {
		t.Fatal(calledModel, result, err)
	}
}
