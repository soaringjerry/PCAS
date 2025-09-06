package openai

import (
    "context"
    "fmt"
    "os"
    "strings"

    "github.com/sashabaranov/go-openai"
    
    "github.com/soaringjerry/pcas/internal/providers"
)

// EmbeddingProvider is an OpenAI implementation of the EmbeddingProvider interface
type EmbeddingProvider struct {
    client *openai.Client
}

// NewEmbeddingProvider creates a new OpenAI embedding provider instance
func NewEmbeddingProvider(apiKey string) providers.EmbeddingProvider {
    // Allow custom base URL via environment variable (e.g., OpenRouter)
    cfg := openai.DefaultConfig(apiKey)
    if base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); base != "" {
        cfg.BaseURL = base
    }
    // If using OpenRouter, attach recommended headers via custom transport
    if strings.Contains(strings.ToLower(cfg.BaseURL), "openrouter.ai") {
        headers := map[string]string{}
        if ref := strings.TrimSpace(os.Getenv("OPENROUTER_SITE_URL")); ref != "" {
            headers["HTTP-Referer"] = ref
        }
        if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_NAME")); title != "" {
            headers["X-Title"] = title
        }
        if len(headers) > 0 {
            cfg.HTTPClient = makeHTTPDoerWithHeaders(headers)
        }
    }
    client := openai.NewClientWithConfig(cfg)
    return &EmbeddingProvider{
        client: client,
    }
}

// CreateEmbedding converts text into a vector embedding using OpenAI's API
func (p *EmbeddingProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
    // Choose embedding model (allow override via env for non-OpenAI endpoints)
    modelName := string(openai.LargeEmbedding3)
    if v := strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_MODEL")); v != "" {
        modelName = v
    }

    // Create embedding request
    req := openai.EmbeddingRequest{
        Input: []string{text},
        Model: openai.EmbeddingModel(modelName),
    }

	// Call OpenAI API
	resp, err := p.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI embedding error: %w", err)
	}

	// Extract embedding
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from OpenAI")
	}

	// Get the first (and only) embedding
	embedding := resp.Data[0].Embedding

	return embedding, nil
}
