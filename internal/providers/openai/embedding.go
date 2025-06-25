package openai

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
	
	"github.com/soaringjerry/pcas/internal/providers"
)

// EmbeddingProvider is an OpenAI implementation of the EmbeddingProvider interface
type EmbeddingProvider struct {
	client *openai.Client
}

// NewEmbeddingProvider creates a new OpenAI embedding provider instance
func NewEmbeddingProvider(apiKey string) providers.EmbeddingProvider {
	client := openai.NewClient(apiKey)
	return &EmbeddingProvider{
		client: client,
	}
}

// CreateEmbedding converts text into a vector embedding using OpenAI's API
func (p *EmbeddingProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Create embedding request
	req := openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.SmallEmbedding3,
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