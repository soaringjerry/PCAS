package storage

import (
	"context"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Storage defines the interface for event storage providers
type Storage interface {
	// StoreEvent persists an event to the storage backend
	StoreEvent(ctx context.Context, event *eventsv1.Event) error
	
	// Close gracefully shuts down the storage connection
	Close() error
}