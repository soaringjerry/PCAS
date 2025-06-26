package storage

import (
	"context"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Storage defines the interface for event storage providers
type Storage interface {
	// StoreEvent persists an event to the storage backend
	StoreEvent(ctx context.Context, event *eventsv1.Event) error
	
	// GetEventByID retrieves a single event by its ID
	GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error)
	
	// BatchGetEvents retrieves multiple events by their IDs in a single query
	BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error)
	
	// Close gracefully shuts down the storage connection
	Close() error
}

// QueryResult represents a single result from a vector similarity query
type QueryResult struct {
	ID    string  // Event ID
	Score float32 // Similarity score (higher is more similar)
}

// VectorStorage defines the interface for vector storage operations
type VectorStorage interface {
	// StoreEmbedding stores a vector embedding for an event
	StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error
	
	// QuerySimilar finds the most similar events based on vector similarity
	QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]QueryResult, error)
	
	// Close gracefully shuts down the vector storage connection
	Close() error
}