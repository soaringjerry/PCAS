package openai

import (
    "context"
    "fmt"
    "io"
    "strconv"
    "strings"

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

    // Create chat completion request (do not force temperature by default)
    req := openai.ChatCompletionRequest{
        Model:    model,
        Messages: messages,
        // Note: omit MaxTokens to avoid 400 on Responses-only models (GPT-5/o4 family)
    }

    // Optional temperature from requestData
    if v, ok := requestData["temperature"]; ok {
        if t, ok := parseTemperature(v); ok {
            if supportsTemperature(model) {
                req.Temperature = float32(t)
            } else {
                // Restricted families accept only default; explicitly set to 1.0
                req.Temperature = 1.0
            }
        }
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

// ExecuteStream implements StreamingComputeProvider using OpenAI Chat Completions streaming.
// It aggregates incoming bytes as a single user prompt, then streams the model output
// back to the caller via the output channel in incremental chunks.
func (p *Provider) ExecuteStream(ctx context.Context, attributes map[string]string, input <-chan []byte, output chan<- []byte) error {
    // Resolve model (attributes override provider default)
    model := p.modelName
    if v, ok := attributes["model"]; ok && strings.TrimSpace(v) != "" {
        model = v
    }

    // Optional system prompt and temperature
    systemPrompt := ""
    if v, ok := attributes["system"]; ok {
        systemPrompt = v
    }

    // Only set temperature if provided; avoid defaulting to non-1 for restricted models
    var (
        haveTemp    bool
        temperature float64
    )
    if v, ok := attributes["temperature"]; ok {
        if t, err := strconv.ParseFloat(v, 64); err == nil {
            temperature = t
            haveTemp = true
        }
    }

    // Read all input chunks into a single user prompt
    var bldr strings.Builder
    for chunk := range input {
        bldr.WriteString(string(chunk))
    }
    userPrompt := bldr.String()

    // Build messages
    msgs := []openai.ChatCompletionMessage{}
    if strings.TrimSpace(systemPrompt) != "" {
        msgs = append(msgs, openai.ChatCompletionMessage{
            Role:    openai.ChatMessageRoleSystem,
            Content: systemPrompt,
        })
    }
    msgs = append(msgs, openai.ChatCompletionMessage{
        Role:    openai.ChatMessageRoleUser,
        Content: userPrompt,
    })

    // Prepare streaming request
    req := openai.ChatCompletionRequest{
        Model:    model,
        Messages: msgs,
        Stream:   true,
        // omit MaxTokens; some models require max_completion_tokens via Responses API
    }
    if haveTemp {
        if supportsTemperature(model) {
            req.Temperature = float32(temperature)
        } else {
            // Restricted families accept only default; explicitly set to 1.0
            req.Temperature = 1.0
        }
    }

    stream, err := p.client.CreateChatCompletionStream(ctx, req)
    if err != nil {
        return fmt.Errorf("OpenAI stream create error: %w", err)
    }
    defer stream.Close()

    for {
        resp, err := stream.Recv()
        if err != nil {
            if err == io.EOF {
                break
            }
            return fmt.Errorf("OpenAI stream recv error: %w", err)
        }
        if len(resp.Choices) == 0 {
            continue
        }
        delta := resp.Choices[0].Delta.Content
        if delta != "" {
            select {
            case output <- []byte(delta):
            case <-ctx.Done():
                return ctx.Err()
            }
        }
    }

    return nil
}

// supportsTemperature determines whether a model family allows non-default temperature values.
// Some responses/realtime/transcription families only accept the default (1.0).
func supportsTemperature(model string) bool {
    m := strings.ToLower(strings.TrimSpace(model))
    if m == "" {
        return true
    }
    // Conservative blocklist; expand as needed
    if strings.HasPrefix(m, "gpt-5") {
        return false
    }
    if strings.HasPrefix(m, "o4") {
        return false
    }
    if strings.Contains(m, "realtime") {
        return false
    }
    if strings.Contains(m, "transcribe") {
        return false
    }
    return true
}

// parseTemperature accepts number or numeric string and returns a float64.
func parseTemperature(v interface{}) (float64, bool) {
    switch t := v.(type) {
    case float32:
        return float64(t), true
    case float64:
        return t, true
    case int:
        return float64(t), true
    case int32:
        return float64(t), true
    case int64:
        return float64(t), true
    case string:
        if s := strings.TrimSpace(t); s != "" {
            if f, err := strconv.ParseFloat(s, 64); err == nil {
                return f, true
            }
        }
    }
    return 0, false
}
