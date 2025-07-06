package storage

import (
	"context"
	"time"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Storage defines the interface for event storage providers
type Storage interface {
	// StoreEvent persists an event to the storage backend
	// The embedding parameter is optional - pass nil if the event has no embedding
	StoreEvent(ctx context.Context, event *eventsv1.Event, embedding []float32) error
	
	// GetEventByID retrieves a single event by its ID
	GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error)
	
	// BatchGetEvents retrieves multiple events by their IDs in a single query
	BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error)
	
	// GetAllEvents retrieves all events with pagination support
	GetAllEvents(ctx context.Context, offset, limit int) ([]*eventsv1.Event, error)
	
	// QuerySimilar finds the most similar events based on vector similarity
	QuerySimilar(ctx context.Context, embedding []float32, topK int, filter *Filter) ([]QueryResult, error)
	
	// AddEmbeddingToEvent adds an embedding to an existing event
	AddEmbeddingToEvent(ctx context.Context, eventID string, embedding []float32) error
	
	// Close gracefully shuts down the storage connection
	Close() error
}

// Filter represents query filter parameters for advanced queries
type Filter struct {
	UserID           *string           // Filter by user ID (nil means no filter)
	SessionID        *string           // Filter by session ID (nil means no filter)
	EventTypes       []string          // Filter by event types (empty slice means no filter)
	TimeFrom         *time.Time        // Filter events after this time (nil means no filter)
	TimeTo           *time.Time        // Filter events before this time (nil means no filter)
	AttributeFilters map[string]string // Filter by event attributes with exact match (AND logic)
}

// QueryResult represents a single result from a vector similarity query
type QueryResult struct {
	ID    string  // Event ID
	Score float32 // Similarity score (higher is more similar)
}