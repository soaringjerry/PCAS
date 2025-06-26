package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/soaringjerry/pcas/internal/providers"
)

const (
	defaultTimeout = 30 * time.Second
	maxRetries     = 2
	retryDelay     = 1 * time.Second
)

// Provider implements the ComputeProvider interface for Ollama
type Provider struct {
	httpClient *http.Client
	baseURL    string
}

// NewProvider creates a new Ollama provider instance
func NewProvider(httpClient *http.Client, baseURL string) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultTimeout,
		}
	}
	
	return &Provider{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// GenerateRequest represents the request payload for Ollama's generate API
type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// GenerateResponse represents the response from Ollama's generate API
type GenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	Context            []int     `json:"context,omitempty"`
	TotalDuration      int64     `json:"total_duration,omitempty"`
	LoadDuration       int64     `json:"load_duration,omitempty"`
	PromptEvalCount    int       `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64     `json:"prompt_eval_duration,omitempty"`
	EvalCount          int       `json:"eval_count,omitempty"`
	EvalDuration       int64     `json:"eval_duration,omitempty"`
}

// Execute processes a request using the Ollama API
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
	startTime := time.Now()
	
	// Extract and validate parameters
	model, prompt, err := p.extractParameters(requestData)
	if err != nil {
		return "", err
	}
	
	log.Printf("OllamaProvider: Starting execution with model=%s", model)
	
	// Prepare the request
	req := GenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false, // PoC doesn't support streaming
	}
	
	// Execute with retry logic
	var response string
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("OllamaProvider: Retry attempt %d after %v delay", attempt, retryDelay)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return "", providers.WrapProviderError(providers.ErrTimeout, ctx.Err())
			}
		}
		
		response, lastErr = p.doRequest(ctx, req)
		if lastErr == nil {
			duration := time.Since(startTime)
			log.Printf("OllamaProvider: Successfully completed in %v", duration)
			return response, nil
		}
		
		// Check if error is retryable
		if !isRetryableError(lastErr) {
			break
		}
	}
	
	log.Printf("OllamaProvider: Failed after %d attempts: %v", maxRetries+1, lastErr)
	return "", lastErr
}

// extractParameters validates and extracts parameters from request data
func (p *Provider) extractParameters(requestData map[string]interface{}) (model string, prompt string, err error) {
	// Extract model (required)
	modelVal, ok := requestData["model"]
	if !ok {
		return "", "", providers.WrapProviderError(
			providers.ErrInvalidInput,
			fmt.Errorf("missing required field: model"),
		)
	}
	
	model, ok = modelVal.(string)
	if !ok || model == "" {
		return "", "", providers.WrapProviderError(
			providers.ErrInvalidInput,
			fmt.Errorf("model must be a non-empty string"),
		)
	}
	
	// Check for unsupported streaming
	if streamVal, ok := requestData["stream"]; ok {
		if stream, _ := streamVal.(bool); stream {
			return "", "", providers.WrapProviderError(
				providers.ErrInvalidInput,
				fmt.Errorf("streaming responses are not supported yet"),
			)
		}
	}
	
	// Extract prompt (required)
	promptVal, ok := requestData["prompt"]
	if !ok {
		return "", "", providers.WrapProviderError(
			providers.ErrInvalidInput,
			fmt.Errorf("missing required field: prompt"),
		)
	}
	
	prompt, ok = promptVal.(string)
	if !ok || prompt == "" {
		return "", "", providers.WrapProviderError(
			providers.ErrInvalidInput,
			fmt.Errorf("prompt must be a non-empty string"),
		)
	}
	
	return model, prompt, nil
}

// doRequest performs a single HTTP request to Ollama
func (p *Provider) doRequest(ctx context.Context, req GenerateRequest) (string, error) {
	// Marshal request
	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", providers.WrapProviderError(providers.ErrInternalError, err)
	}
	
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s/api/generate", p.baseURL),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", providers.WrapProviderError(providers.ErrInternalError, err)
	}
	
	httpReq.Header.Set("Content-Type", "application/json")
	
	// Execute request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		// Network errors are typically retryable
		return "", providers.WrapProviderError(providers.ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()
	
	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return "", providers.WrapProviderError(
				providers.ErrUnauthorized,
				fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
			)
		case http.StatusTooManyRequests:
			return "", providers.WrapProviderError(
				providers.ErrRateLimited,
				fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
			)
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
			// These are retryable
			return "", providers.WrapProviderError(
				providers.ErrProviderUnavailable,
				fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
			)
		default:
			return "", providers.WrapProviderError(
				providers.ErrInternalError,
				fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body)),
			)
		}
	}
	
	// Parse response
	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", providers.WrapProviderError(
			providers.ErrInternalError,
			fmt.Errorf("failed to decode response: %w", err),
		)
	}
	
	if !genResp.Done {
		return "", providers.WrapProviderError(
			providers.ErrInternalError,
			fmt.Errorf("incomplete response from Ollama"),
		)
	}
	
	return genResp.Response, nil
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	// Check if it's a wrapped provider unavailable error
	if providers.ErrProviderUnavailable.Error() == err.Error() {
		return true
	}
	
	// Check if the error contains certain patterns
	errStr := err.Error()
	return contains(errStr, "provider service is unavailable") ||
		contains(errStr, "connection refused") ||
		contains(errStr, "timeout")
}

// contains is a simple string contains helper
func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}