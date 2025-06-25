package ollama

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/pcas/internal/providers"
)

func TestProvider_Execute_Success(t *testing.T) {
	// Create a test server that simulates Ollama API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/api/generate" {
			t.Errorf("Expected path /api/generate, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		
		// Return successful response
		response := `{
			"model": "llama3:8b",
			"created_at": "2024-06-25T12:00:00Z",
			"response": "Hello! I'm a local LLM running through Ollama.",
			"done": true,
			"total_duration": 1000000000,
			"eval_count": 10,
			"eval_duration": 500000000
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()
	
	// Create provider with test server URL
	provider := NewProvider(nil, server.URL)
	
	// Test request
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Say hello",
	}
	
	ctx := context.Background()
	response, err := provider.Execute(ctx, requestData)
	
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	
	expected := "Hello! I'm a local LLM running through Ollama."
	if response != expected {
		t.Errorf("Expected response %q, got %q", expected, response)
	}
}

func TestProvider_Execute_WithModel(t *testing.T) {
	modelReceived := ""
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the model from request
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		if strings.Contains(string(body), "llama3:70b") {
			modelReceived = "llama3:70b"
		}
		
		response := `{"model": "llama3:70b", "response": "Response from 70B model", "done": true}`
		w.Write([]byte(response))
	}))
	defer server.Close()
	
	provider := NewProvider(nil, server.URL)
	
	requestData := map[string]interface{}{
		"prompt": "Test prompt",
		"model":  "llama3:70b",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	
	if modelReceived != "llama3:70b" {
		t.Errorf("Expected model llama3:70b to be sent, but it wasn't detected")
	}
}

func TestProvider_Execute_MissingPrompt(t *testing.T) {
	provider := NewProvider(nil, "http://localhost:11434")
	
	// Request without prompt
	requestData := map[string]interface{}{
		"model": "llama3:8b",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected error for missing prompt, got nil")
	}
	
	if !strings.Contains(err.Error(), "missing required field: prompt") {
		t.Errorf("Expected error about missing prompt, got: %v", err)
	}
}

func TestProvider_Execute_MissingModel(t *testing.T) {
	provider := NewProvider(nil, "http://localhost:11434")
	
	// Request without model
	requestData := map[string]interface{}{
		"prompt": "Test prompt",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected error for missing model, got nil")
	}
	
	if !strings.Contains(err.Error(), "missing required field: model") {
		t.Errorf("Expected error about missing model, got: %v", err)
	}
}

func TestProvider_Execute_StreamingNotSupported(t *testing.T) {
	provider := NewProvider(nil, "http://localhost:11434")
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test",
		"stream": true,
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected error for streaming request, got nil")
	}
	
	if !strings.Contains(err.Error(), "streaming responses are not supported") {
		t.Errorf("Expected error about streaming not supported, got: %v", err)
	}
}

func TestProvider_Execute_ServerError_WithRetry(t *testing.T) {
	attempts := 0
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		
		if attempts <= 2 {
			// Return server error for first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
		} else {
			// Success on third attempt
			response := `{"model": "llama3:8b", "response": "Success after retry", "done": true}`
			w.Write([]byte(response))
		}
	}))
	defer server.Close()
	
	// Create provider with custom HTTP client for faster retries
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}
	provider := NewProvider(httpClient, server.URL)
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test retry",
	}
	
	response, err := provider.Execute(context.Background(), requestData)
	
	if err != nil {
		t.Fatalf("Execute returned error after retries: %v", err)
	}
	
	if response != "Success after retry" {
		t.Errorf("Expected successful response after retry, got: %s", response)
	}
	
	if attempts != 3 {
		t.Errorf("Expected 3 attempts (initial + 2 retries), got %d", attempts)
	}
}

func TestProvider_Execute_NonRetryableError(t *testing.T) {
	attempts := 0
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Return 400 Bad Request - not retryable
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Bad request"))
	}))
	defer server.Close()
	
	provider := NewProvider(nil, server.URL)
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	
	// Should only try once for non-retryable errors
	if attempts != 1 {
		t.Errorf("Expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestProvider_Execute_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"response": "Too late", "done": true}`))
	}))
	defer server.Close()
	
	// Create provider with very short timeout
	httpClient := &http.Client{
		Timeout: 10 * time.Millisecond,
	}
	provider := NewProvider(httpClient, server.URL)
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test timeout",
	}
	
	ctx := context.Background()
	_, err := provider.Execute(ctx, requestData)
	
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
	
	if !strings.Contains(err.Error(), "provider service is unavailable") {
		t.Errorf("Expected provider unavailable error, got: %v", err)
	}
}

func TestProvider_Execute_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("Rate limit exceeded"))
	}))
	defer server.Close()
	
	provider := NewProvider(nil, server.URL)
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test rate limit",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected rate limit error, got nil")
	}
	
	if !strings.Contains(err.Error(), providers.ErrRateLimited.Error()) {
		t.Errorf("Expected rate limited error, got: %v", err)
	}
}

func TestProvider_Execute_IncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return response with done=false
		response := `{
			"model": "llama3:8b",
			"response": "Partial response",
			"done": false
		}`
		w.Write([]byte(response))
	}))
	defer server.Close()
	
	provider := NewProvider(nil, server.URL)
	
	requestData := map[string]interface{}{
		"model":  "llama3:8b",
		"prompt": "Test",
	}
	
	_, err := provider.Execute(context.Background(), requestData)
	
	if err == nil {
		t.Fatal("Expected error for incomplete response, got nil")
	}
	
	if !strings.Contains(err.Error(), "incomplete response") {
		t.Errorf("Expected incomplete response error, got: %v", err)
	}
}

// TestIsRetryableError tests the retry logic
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Provider unavailable",
			err:      providers.ErrProviderUnavailable,
			expected: true,
		},
		{
			name:     "Connection refused",
			err:      fmt.Errorf("connection refused"),
			expected: true,
		},
		{
			name:     "Timeout error",
			err:      fmt.Errorf("request timeout"),
			expected: true,
		},
		{
			name:     "Invalid input",
			err:      providers.ErrInvalidInput,
			expected: false,
		},
		{
			name:     "Random error",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}