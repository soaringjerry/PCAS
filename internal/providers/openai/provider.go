package openai

import (
    "context"
    "fmt"

    "github.com/sashabaranov/go-openai"
)

// Provider is an OpenAI implementation of ComputeProvider
type Provider struct {
    client    *openai.Client
    modelName string
}

const defaultChatModel = "gpt-5" // default to GPT-5; can be overridden per provider via policy

// NewProvider creates a new OpenAI provider instance with default model
func NewProvider(apiKey string) *Provider {
    return NewProviderWithModel(apiKey, defaultChatModel)
}

// NewProviderWithModel creates a new OpenAI provider with an explicit model name
func NewProviderWithModel(apiKey, model string) *Provider {
    client := openai.NewClient(apiKey)
    if model == "" {
        model = defaultChatModel
    }
    return &Provider{
        client:    client,
        modelName: model,
    }
}

// Execute implements the ComputeProvider interface
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
	var messages []openai.ChatCompletionMessage
	
	// Debug: log what we received
	if ragApplied, exists := requestData["rag_applied"]; exists {
		fmt.Printf("OpenAI: RAG applied = %v\n", ragApplied)
	}
	
	// Check if messages are already provided (RAG enhanced)
	if messagesInterface, exists := requestData["messages"]; exists {
		// Convert messages from interface{} to proper format
		if msgSlice, ok := messagesInterface.([]map[string]string); ok {
			for _, msg := range msgSlice {
				role := msg["role"]
				content := msg["content"]
				
				var openaiRole string
				switch role {
				case "system":
					openaiRole = openai.ChatMessageRoleSystem
				case "user":
					openaiRole = openai.ChatMessageRoleUser
				case "assistant":
					openaiRole = openai.ChatMessageRoleAssistant
				default:
					openaiRole = openai.ChatMessageRoleUser
				}
				
				messages = append(messages, openai.ChatCompletionMessage{
					Role:    openaiRole,
					Content: content,
				})
			}
			fmt.Printf("OpenAI: Using RAG-enhanced messages with %d messages\n", len(messages))
		} else {
			return "", fmt.Errorf("'messages' field has invalid format")
		}
	} else {
		// Fall back to simple prompt format
		promptInterface, exists := requestData["prompt"]
		if !exists {
			return "", fmt.Errorf("no 'prompt' or 'messages' field found in request data")
		}
		
		prompt, ok := promptInterface.(string)
		if !ok {
			return "", fmt.Errorf("'prompt' field is not a string")
		}
		
		messages = []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		}
	}
	
    // Determine model (allow request override via `model` field; fallback to provider default)
    model := p.modelName
    if v, ok := requestData["model"]; ok {
        if s, ok := v.(string); ok && s != "" {
            model = s
        }
    }

    // Create chat completion request
    req := openai.ChatCompletionRequest{
        Model:       model,
        Messages:    messages,
        Temperature: 0.7,
        MaxTokens:   2000, // Increased for longer responses with context
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
