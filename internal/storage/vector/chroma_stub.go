//go:build no_chroma

package vector

import (
	"context"
	"fmt"
	
	"github.com/soaringjerry/pcas/internal/storage"
)

// ChromaProvider implements the VectorStorage interface using ChromaDB
type ChromaProvider struct{}

// NewChromaProvider creates a new ChromaDB vector storage provider
func NewChromaProvider(chromaURL string) (storage.VectorStorage, error) {
	return nil, fmt.Errorf("ChromaDB support is disabled in this build")
}

// StoreEmbedding stores a vector embedding for an event
func (c *ChromaProvider) StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error {
	return fmt.Errorf("ChromaDB support is disabled")
}

// QuerySimilar finds the most similar events based on vector similarity
func (c *ChromaProvider) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	return nil, fmt.Errorf("ChromaDB support is disabled")
}

// Close gracefully shuts down the vector storage connection
func (c *ChromaProvider) Close() error {
	return nil
}