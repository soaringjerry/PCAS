package bus

import (
	"github.com/soaringjerry/pcas/internal/providers"
	"github.com/soaringjerry/pcas/internal/storage"
)

// SetVectorStorage sets the vector storage provider
func (s *Server) SetVectorStorage(vectorStorage storage.VectorStorage) {
	s.vectorStorage = vectorStorage
}

// SetEmbeddingProvider sets the embedding provider
func (s *Server) SetEmbeddingProvider(embeddingProvider providers.EmbeddingProvider) {
	s.embeddingProvider = embeddingProvider
}