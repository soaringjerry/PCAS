package memory

import (
	"context"
	"log"
	"sync"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MemoryBus implements an in-memory event bus
type MemoryBus struct {
	busv1.UnimplementedEventBusServiceServer
	
	// subscribers maps client IDs to their event channels
	subscribers map[string]chan *eventsv1.Event
	// mu protects concurrent access to subscribers map
	mu sync.RWMutex
}

// NewMemoryBus creates a new in-memory event bus instance
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subscribers: make(map[string]chan *eventsv1.Event),
	}
}

// Publish broadcasts an event to all subscribers
func (b *MemoryBus) Publish(ctx context.Context, event *eventsv1.Event) (*busv1.PublishResponse, error) {
	if event == nil {
		return nil, status.Error(codes.InvalidArgument, "event cannot be nil")
	}

	// Get read lock to safely iterate over subscribers
	b.mu.RLock()
	defer b.mu.RUnlock()

	var successCount int32
	for clientID, ch := range b.subscribers {
		// Non-blocking send to prevent slow subscribers from blocking the bus
		select {
		case ch <- event:
			successCount++
		default:
			// Channel is full, log warning and skip
			log.Printf("Warning: dropping event for slow subscriber %s (channel full)", clientID)
		}
	}

	return &busv1.PublishResponse{}, nil
}

// Subscribe creates a subscription stream for a client
func (b *MemoryBus) Subscribe(req *busv1.SubscribeRequest, stream grpc.ServerStreamingServer[eventsv1.Event]) error {
	if req.ClientId == "" {
		return status.Error(codes.InvalidArgument, "client_id cannot be empty")
	}

	// Create a buffered channel for this subscriber
	eventCh := make(chan *eventsv1.Event, 100)

	// Register the subscriber
	b.mu.Lock()
	if _, exists := b.subscribers[req.ClientId]; exists {
		b.mu.Unlock()
		return status.Errorf(codes.AlreadyExists, "client %s is already subscribed", req.ClientId)
	}
	b.subscribers[req.ClientId] = eventCh
	b.mu.Unlock()

	// Ensure cleanup on exit
	defer func() {
		b.mu.Lock()
		delete(b.subscribers, req.ClientId)
		close(eventCh)
		b.mu.Unlock()
		log.Printf("Client %s unsubscribed", req.ClientId)
	}()

	log.Printf("Client %s subscribed", req.ClientId)

	// Stream events to the client
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, exit
				return nil
			}
			
			// Send event to client
			if err := stream.Send(event); err != nil {
				log.Printf("Error sending event to client %s: %v", req.ClientId, err)
				return err
			}
			
		case <-stream.Context().Done():
			// Client disconnected
			return nil
		}
	}
}

// Search is not implemented for the memory bus as it doesn't store historical events
func (b *MemoryBus) Search(ctx context.Context, req *busv1.SearchRequest) (*busv1.SearchResponse, error) {
	return nil, status.Error(codes.Unimplemented, "memory bus does not support semantic search")
}

// GetSubscriberCount returns the current number of active subscribers (useful for testing)
func (b *MemoryBus) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}