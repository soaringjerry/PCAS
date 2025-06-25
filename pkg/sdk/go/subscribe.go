package pcas

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/google/uuid"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Subscribe creates a subscription to the PCAS event stream
// Returns a read-only channel that will receive all events
func (c *Client) Subscribe(ctx context.Context) (<-chan *eventsv1.Event, error) {
	// Generate a unique client ID
	clientID := fmt.Sprintf("pcas-sdk-%s", uuid.New().String()[:8])

	// Create subscription request
	req := &busv1.SubscribeRequest{
		ClientId: clientID,
	}

	// Start subscription stream
	stream, err := c.grpcClient.Subscribe(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe: %w", err)
	}

	// Create event channel
	eventChan := make(chan *eventsv1.Event)

	// Start background goroutine to handle stream
	go func() {
		defer close(eventChan)
		
		for {
			// Receive event from stream
			event, err := stream.Recv()
			if err != nil {
				// Check if context was cancelled
				if ctx.Err() != nil {
					// Normal shutdown
					return
				}
				// Check for end of stream
				if err == io.EOF {
					return
				}
				// Log error and exit
				log.Printf("Error receiving event: %v", err)
				return
			}

			// Send event to channel
			select {
			case eventChan <- event:
				// Event sent successfully
			case <-ctx.Done():
				// Context cancelled, exit
				return
			}
		}
	}()

	return eventChan, nil
}