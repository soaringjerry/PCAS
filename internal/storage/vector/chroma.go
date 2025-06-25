package vector

import (
	"context"
	"fmt"
	"log"

	chromago "github.com/amikos-tech/chroma-go"
	"github.com/amikos-tech/chroma-go/types"
	
	"github.com/soaringjerry/pcas/internal/storage"
)

// ChromaProvider implements the VectorStorage interface using ChromaDB
type ChromaProvider struct {
	client     *chromago.Client
	collection *chromago.Collection
}

// NewChromaProvider creates a new ChromaDB vector storage provider
func NewChromaProvider(chromaURL string) (storage.VectorStorage, error) {
	// Create ChromaDB client
	client, err := chromago.NewClient(chromago.WithBasePath(chromaURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create ChromaDB client: %w", err)
	}

	// Get or create collection
	collectionName := "pcas-events"
	
	// Try to get existing collection first
	coll, err := client.GetCollection(
		context.Background(),
		collectionName,
		nil,
	)
	if err != nil {
		// Collection doesn't exist, create it
		log.Printf("Creating new ChromaDB collection: %s", collectionName)
		
		// Create collection with cosine distance metric
		coll, err = client.CreateCollection(
			context.Background(),
			collectionName,
			map[string]interface{}{
				"description": "PCAS event embeddings",
			},
			true, // Create if not exists
			nil, // No embedding function
			types.COSINE, // Distance function
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create ChromaDB collection: %w", err)
		}
	} else {
		log.Printf("Using existing ChromaDB collection: %s", collectionName)
	}

	return &ChromaProvider{
		client:     client,
		collection: coll,
	}, nil
}

// StoreEmbedding stores a vector embedding for an event
func (p *ChromaProvider) StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error {
	// Convert metadata to map[string]interface{} as required by ChromaDB
	metadataInterface := make(map[string]interface{})
	for k, v := range metadata {
		metadataInterface[k] = v
	}

	// Convert embedding to ChromaDB format
	embeddings := types.NewEmbeddingsFromFloat32([][]float32{embedding})
	
	// Add to collection
	_, err := p.collection.Add(
		ctx,
		embeddings,
		[]map[string]interface{}{metadataInterface},
		nil, // No documents, we store everything in metadata
		[]string{eventID},
	)
	
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	log.Printf("Stored embedding for event %s in ChromaDB", eventID)
	return nil
}

// QuerySimilar finds the most similar events based on vector similarity
func (p *ChromaProvider) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	// Convert query embedding to ChromaDB format
	queryEmbeddingChroma := types.NewEmbeddingFromFloat32(queryEmbedding)
	
	// Query the collection with embeddings
	results, err := p.collection.QueryWithOptions(
		ctx,
		types.WithQueryEmbedding(queryEmbeddingChroma),
		types.WithNResults(int32(topK)),
		types.WithInclude(types.IDistances, types.IMetadatas),
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to query similar embeddings: %w", err)
	}

	// Extract event IDs from results
	var eventIDs []string
	if results != nil && len(results.Ids) > 0 && len(results.Ids[0]) > 0 {
		eventIDs = results.Ids[0]
	}

	return eventIDs, nil
}

// Close gracefully shuts down the vector storage connection
func (p *ChromaProvider) Close() error {
	// ChromaDB Go client doesn't require explicit closing
	return nil
}