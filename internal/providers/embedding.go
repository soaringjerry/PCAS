package providers

import (
	"context"
)

// EmbeddingProvider defines the interface for text embedding providers
type EmbeddingProvider interface {
	// CreateEmbedding converts text into a vector embedding
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
}