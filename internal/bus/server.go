package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/policy"
	"github.com/soaringjerry/pcas/internal/providers"
)

// Server implements the EventBusService gRPC server
type Server struct {
	busv1.UnimplementedEventBusServiceServer
	policyEngine *policy.Engine
	providers    map[string]providers.ComputeProvider
	
	// Subscriber management
	subscribers map[string]chan *eventsv1.Event
	subMutex    sync.RWMutex
}

// NewServer creates a new bus server instance
func NewServer(policyEngine *policy.Engine, providerMap map[string]providers.ComputeProvider) *Server {
	return &Server{
		policyEngine: policyEngine,
		providers:    providerMap,
		subscribers:  make(map[string]chan *eventsv1.Event),
	}
}

// Publish handles incoming events from clients
func (s *Server) Publish(ctx context.Context, event *eventsv1.Event) (*busv1.PublishResponse, error) {
	log.Printf("Received event: ID=%s, Type=%s, Source=%s", event.Id, event.Type, event.Source)
	
	if event.Subject != "" {
		log.Printf("  Subject: %s", event.Subject)
	}
	
	// Extract event data if present
	var requestData map[string]interface{}
	if event.Data != nil {
		// Check if the data is a structpb.Value
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			// Unmarshal the Any into the Value
			if err := event.Data.UnmarshalTo(value); err != nil {
				log.Printf("  Data: <failed to unmarshal: %v>", err)
			} else {
				// Convert to map
				if mapData, ok := value.AsInterface().(map[string]interface{}); ok {
					requestData = mapData
				}
				// Log the data
				jsonBytes, err := json.MarshalIndent(value.AsInterface(), "    ", "  ")
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
	
	// Use policy engine to select provider
	providerName := s.policyEngine.SelectProvider(event)
	if providerName == "" {
		log.Printf("No provider configured for event type: %s", event.Type)
		return &busv1.PublishResponse{}, nil
	}
	
	log.Printf("Selected provider: %s", providerName)
	
	// Get the provider instance
	provider, exists := s.providers[providerName]
	if !exists {
		return nil, fmt.Errorf("provider not found: %s", providerName)
	}
	
	// Execute the provider
	result, err := provider.Execute(ctx, requestData)
	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}
	
	log.Printf("Provider response: %s", result)
	
	// Create a response event
	responseEvent := &eventsv1.Event{
		Id:        uuid.New().String(),
		Type:      "pcas.response.v1",
		Source:    "pcas-server",
		Specversion: "1.0",
		Time:      timestamppb.New(time.Now()),
		Subject:   fmt.Sprintf("response-to-%s", event.Id),
	}
	
	// Add the response data
	responseData := map[string]interface{}{
		"original_event_id": event.Id,
		"provider":          providerName,
		"response":          result,
	}
	
	structData, err := structpb.NewValue(responseData)
	if err != nil {
		log.Printf("Failed to create response data: %v", err)
	} else {
		responseEvent.Data, _ = anypb.New(structData)
	}
	
	// Broadcast the response event to all subscribers
	s.broadcastEvent(responseEvent)
	
	return &busv1.PublishResponse{}, nil
}