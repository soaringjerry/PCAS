package openai

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// Provider is an OpenAI implementation of ComputeProvider
type Provider struct {
	client *openai.Client
}

// NewProvider creates a new OpenAI provider instance
func NewProvider(apiKey string) *Provider {
	client := openai.NewClient(apiKey)
	return &Provider{
		client: client,
	}
}

// Execute implements the ComputeProvider interface
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
	// Extract prompt from request data
	promptInterface, exists := requestData["prompt"]
	if !exists {
		return "", fmt.Errorf("no 'prompt' field found in request data")
	}
	
	prompt, ok := promptInterface.(string)
	if !ok {
		return "", fmt.Errorf("'prompt' field is not a string")
	}
	
	// Create chat completion request
	req := openai.ChatCompletionRequest{
		Model: openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   1000,
	}
	
	// Call OpenAI API
	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}
	
	// Extract response content
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices from OpenAI")
	}
	
	return resp.Choices[0].Message.Content, nil
}