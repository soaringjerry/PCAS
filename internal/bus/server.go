package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/policy"
	"github.com/soaringjerry/pcas/internal/providers"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Server implements the EventBusService gRPC server
type Server struct {
	busv1.UnimplementedEventBusServiceServer
	policyEngine *policy.Engine
	providers    map[string]providers.ComputeProvider
	storage      storage.Storage
	embeddingProvider providers.EmbeddingProvider
	
	// Subscriber management
	subscribers map[string]chan *eventsv1.Event
	subMutex    sync.RWMutex
	
	// RAG enhancement fields
	embeddingCache    *embeddingCache
	rateLimiter       *rate.Limiter
	singleFlight      *singleflight.Group
	
	// Background task tracking
	vectorizeWG  sync.WaitGroup
}

// NewServer creates a new bus server instance
func NewServer(policyEngine *policy.Engine, providerMap map[string]providers.ComputeProvider, storage storage.Storage) *Server {
	return &Server{
		policyEngine: policyEngine,
		providers:    providerMap,
		storage:      storage,
		subscribers:  make(map[string]chan *eventsv1.Event),
		embeddingCache: newEmbeddingCache(1000), // LRU cache for 1000 embeddings
		rateLimiter:    rate.NewLimiter(rate.Every(time.Second), 10), // 10 requests per second
		singleFlight:   &singleflight.Group{},
	}
}

// Publish handles incoming events from clients
func (s *Server) Publish(ctx context.Context, event *eventsv1.Event) (*busv1.PublishResponse, error) {
	// Store the incoming event immediately
	if err := s.storage.StoreEvent(ctx, event, nil); err != nil {
		log.Printf("Failed to store incoming event: %v", err)
		// Continue processing even if storage fails
	}
	
	// Start vectorization in background if providers are available
	// Only vectorize fact events
	if s.embeddingProvider != nil {
		if IsFactEvent(event.Type) {
			log.Printf("Will vectorize fact event: type=%s, id=%s", event.Type, event.Id)
			s.vectorizeWG.Add(1)  // Increment counter before starting goroutine
			go s.vectorizeEvent(event)
		} else {
			log.Printf("Skipping vectorization for non-fact event: type=%s, id=%s", event.Type, event.Id)
		}
	}
	
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
	providerName, promptTemplate := s.policyEngine.SelectProvider(event)
	if providerName == "" {
		log.Printf("No provider configured for event type: %s", event.Type)
		return &busv1.PublishResponse{}, nil
	}
	
	log.Printf("Selected provider: %s", providerName)
	if promptTemplate != "" {
		log.Printf("Using prompt template: %s", promptTemplate)
	}
	
	// Get the provider instance
	provider, exists := s.providers[providerName]
	if !exists {
		return nil, fmt.Errorf("provider not found: %s", providerName)
	}
	
	// Apply RAG enhancement only for LLM providers
	if providerName == "openai-gpt4" && s.embeddingProvider != nil && s.storage != nil {
		s.applyRAGEnhancement(ctx, event, requestData)
	}
	
	// Execute the provider
	result, err := provider.Execute(ctx, requestData)
	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}
	
	log.Printf("Provider response: %s", result)
	
	// Create a response event
	responseEvent := &eventsv1.Event{
		Id:            uuid.New().String(),
		Type:          "pcas.response.v1",
		Source:        "pcas-server",
		Specversion:   "1.0",
		Time:          timestamppb.New(time.Now()),
		Subject:       fmt.Sprintf("response-to-%s", event.Id),
		TraceId:       event.TraceId,        // Pass through the trace ID
		CorrelationId: event.Id,              // Set correlation to the original event ID
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
	
	// Store the response event before broadcasting
	if err := s.storage.StoreEvent(ctx, responseEvent, nil); err != nil {
		log.Printf("Failed to store response event: %v", err)
		// Continue processing even if storage fails
	}
	
	// Don't vectorize response events - they don't contain user intent
	// Only user-generated content should be in vector space
	
	// Broadcast the response event to all subscribers
	s.broadcastEvent(responseEvent)
	
	return &busv1.PublishResponse{}, nil
}

// Search performs semantic search across stored events
func (s *Server) Search(ctx context.Context, req *busv1.SearchRequest) (*busv1.SearchResponse, error) {
	// Validate request
	if req.QueryText == "" {
		return nil, fmt.Errorf("query_text cannot be empty")
	}
	
	if req.TopK <= 0 {
		req.TopK = 5 // Default to 5 results
	}
	
	// Check if embedding provider is available
	if s.embeddingProvider == nil {
		return nil, fmt.Errorf("vector search is not available on the server. Please ensure the PCAS server was started with the OPENAI_API_KEY environment variable set")
	}
	
	// Create embedding for the query text
	log.Printf("Creating embedding for search query: %s", req.QueryText)
	queryEmbedding, err := s.embeddingProvider.CreateEmbedding(ctx, req.QueryText)
	if err != nil {
		return nil, fmt.Errorf("failed to create query embedding: %w", err)
	}
	
	// Build filter if user_id is provided
	var filter *storage.Filter
	if req.UserId != "" {
		filter = &storage.Filter{
			UserID: &req.UserId,
		}
		log.Printf("Applying user filter: %s", req.UserId)
	}
	
	// Query similar events from storage
	log.Printf("Searching for top %d similar events", req.TopK)
	eventIDs, err := s.storage.QuerySimilar(ctx, queryEmbedding, int(req.TopK), filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar events: %w", err)
	}
	
	// Retrieve full event details from storage
	var results []*eventsv1.Event
	var scores []float32
	for _, result := range eventIDs {
		event, err := s.storage.GetEventByID(ctx, result.ID)
		if err != nil {
			log.Printf("Warning: failed to retrieve event %s: %v", result.ID, err)
			continue
		}
		results = append(results, event)
		scores = append(scores, result.Score)
		log.Printf("Retrieved event %s with similarity score: %.3f", result.ID, result.Score)
	}
	
	log.Printf("Search completed: found %d matching events", len(results))
	
	return &busv1.SearchResponse{
		Events: results,
		Scores: scores,
	}, nil
}

// SetEmbeddingProvider sets the embedding provider for the server
func (s *Server) SetEmbeddingProvider(provider providers.EmbeddingProvider) {
	s.embeddingProvider = provider
}

// WaitForVectorization waits for all background vectorization tasks to complete
func (s *Server) WaitForVectorization() {
	s.vectorizeWG.Wait()
}

// isFactEvent determines if an event type represents a "fact" that should be vectorized
