package vector

import (
	"context"
	"fmt"
	"log"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	
	"github.com/soaringjerry/pcas/internal/storage"
)

// ChromaProvider implements the VectorStorage interface using ChromaDB
type ChromaProvider struct {
	client     chroma.Client
	collection chroma.Collection
}

// NewChromaProvider creates a new ChromaDB vector storage provider
func NewChromaProvider(chromaURL string) (storage.VectorStorage, error) {
	// Create ChromaDB v2 client
	client, err := chroma.NewHTTPClient(chroma.WithBaseURL(chromaURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create ChromaDB client: %w", err)
	}

	// Get or create collection
	collectionName := "pcas-events"
	
	// Create metadata for collection
	metadata := chroma.NewMetadata()
	metadata.SetString("description", "PCAS event embeddings")
	
	// Use GetOrCreateCollection for v2 API
	coll, err := client.GetOrCreateCollection(
		context.Background(),
		collectionName,
		chroma.WithCollectionMetadataCreate(metadata),
		chroma.WithHNSWSpaceCreate(embeddings.COSINE),
		chroma.WithEmbeddingFunctionCreate(embeddings.NewConsistentHashEmbeddingFunction()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create ChromaDB collection: %w", err)
	}
	
	log.Printf("Using ChromaDB collection: %s", collectionName)

	return &ChromaProvider{
		client:     client,
		collection: coll,
	}, nil
}

// StoreEmbedding stores a vector embedding for an event
func (p *ChromaProvider) StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error {
	// Convert metadata to DocumentMetadata
	docMeta, err := chroma.NewDocumentMetadataFromMap(convertStringMapToInterface(metadata))
	if err != nil {
		return fmt.Errorf("failed to convert metadata: %w", err)
	}

	// Convert embedding to ChromaDB v2 format
	embeddingV2 := embeddings.NewEmbeddingFromFloat32(embedding)
	
	// Add to collection using v2 API
	err = p.collection.Add(
		ctx,
		chroma.WithIDs(chroma.DocumentID(eventID)),
		chroma.WithEmbeddings(embeddingV2),
		chroma.WithMetadatas(docMeta),
	)
	
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	log.Printf("Stored embedding for event %s in ChromaDB", eventID)
	return nil
}

// QuerySimilar finds the most similar events based on vector similarity
func (p *ChromaProvider) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	// Convert query embedding to ChromaDB v2 format
	queryEmbeddingV2 := embeddings.NewEmbeddingFromFloat32(queryEmbedding)
	
	// Query the collection using v2 API
	results, err := p.collection.Query(
		ctx,
		chroma.WithQueryEmbeddings(queryEmbeddingV2),
		chroma.WithNResults(topK),
		chroma.WithIncludeQuery(chroma.IncludeMetadatas),
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to query similar embeddings: %w", err)
	}

	// Extract event IDs from results
	var eventIDs []string
	if results != nil && results.CountGroups() > 0 {
		idGroups := results.GetIDGroups()
		if len(idGroups) > 0 && len(idGroups[0]) > 0 {
			for _, id := range idGroups[0] {
				eventIDs = append(eventIDs, string(id))
			}
		}
	}

	return eventIDs, nil
}

// Close gracefully shuts down the vector storage connection
func (p *ChromaProvider) Close() error {
	// ChromaDB v2 client has a Close method
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

// convertStringMapToInterface converts map[string]string to map[string]interface{}
func convertStringMapToInterface(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}