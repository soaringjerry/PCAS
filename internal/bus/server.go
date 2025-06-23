package bus

import (
	"context"
	"encoding/json"
	"log"

	"google.golang.org/protobuf/types/known/structpb"
	
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Server implements the EventBusService gRPC server
type Server struct {
	busv1.UnimplementedEventBusServiceServer
}

// NewServer creates a new bus server instance
func NewServer() *Server {
	return &Server{}
}

// Publish handles incoming events from clients
func (s *Server) Publish(ctx context.Context, event *eventsv1.Event) (*busv1.PublishResponse, error) {
	log.Printf("Received event: ID=%s, Type=%s, Source=%s", event.Id, event.Type, event.Source)
	
	if event.Subject != "" {
		log.Printf("  Subject: %s", event.Subject)
	}
	
	if event.Data != nil {
		// Check if the data is a structpb.Value and format it as JSON
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			// Unmarshal the Any into the Value
			if err := event.Data.UnmarshalTo(value); err != nil {
				log.Printf("  Data: <failed to unmarshal: %v>", err)
			} else {
				// Convert to interface{} and then to formatted JSON
				dataInterface := value.AsInterface()
				jsonBytes, err := json.MarshalIndent(dataInterface, "    ", "  ")
				if err != nil {
					log.Printf("  Data: <failed to format as JSON: %v>", err)
				} else {
					log.Printf("  Data: %s", string(jsonBytes))
				}
			}
		} else {
			// Fall back to raw string representation for non-Value types
			log.Printf("  Data: %s", event.Data.String())
		}
	}
	
	// For now, just acknowledge receipt
	// In the future, this would route the event to handlers
	
	return &busv1.PublishResponse{}, nil
}