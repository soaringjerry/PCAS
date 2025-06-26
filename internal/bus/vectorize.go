package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// vectorizeEvent extracts text content from an event and stores its embedding
func (s *Server) vectorizeEvent(event *eventsv1.Event) {
	// Create a context with timeout for vectorization
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Extract text content to vectorize
	textContent := s.extractTextContent(event)
	if textContent == "" {
		// No text content to vectorize
		return
	}
	
	log.Printf("Vectorizing content for event %s (type: %s): \"%s\"", event.Id, event.Type, textContent)

	// Create embedding
	embedding, err := s.embeddingProvider.CreateEmbedding(ctx, textContent)
	if err != nil {
		log.Printf("Failed to create embedding for event %s: %v", event.Id, err)
		return
	}

	// Prepare metadata
	metadata := map[string]string{
		"event_type":   event.Type,
		"event_source": event.Source,
	}
	
	// Add timestamp_unix (required for filtering)
	if event.Time != nil {
		metadata["timestamp_unix"] = fmt.Sprintf("%d", event.Time.AsTime().Unix())
		metadata["timestamp"] = event.Time.AsTime().Format(time.RFC3339)
	}
	
	// Add user_id if present
	if event.UserId != "" {
		metadata["user_id"] = event.UserId
	}
	
	// Add session_id if present
	if event.SessionId != "" {
		metadata["session_id"] = event.SessionId
	}
	
	if event.TraceId != "" {
		metadata["trace_id"] = event.TraceId
	}
	
	if event.CorrelationId != "" {
		metadata["correlation_id"] = event.CorrelationId
	}

	// Store embedding
	err = s.vectorStorage.StoreEmbedding(ctx, event.Id, embedding, metadata)
	if err != nil {
		log.Printf("Failed to store embedding for event %s: %v", event.Id, err)
		return
	}

	log.Printf("Successfully vectorized event %s (type: %s)", event.Id, event.Type)
}

// extractTextContent extracts meaningful text from event data
func (s *Server) extractTextContent(event *eventsv1.Event) string {
	// First priority: Check event.Subject
	if event.Subject != "" {
		log.Printf("Extracting text from Subject field: \"%s\"", event.Subject)
		return event.Subject
	}
	
	// If no subject, try to extract from event.Data
	if event.Data == nil {
		return ""
	}

	// Try to unmarshal as structpb.Value
	value := &structpb.Value{}
	if !event.Data.MessageIs(value) {
		return ""
	}

	if err := event.Data.UnmarshalTo(value); err != nil {
		return ""
	}

	// Convert to map
	data, ok := value.AsInterface().(map[string]interface{})
	if !ok {
		return ""
	}

	// Extract text based on event type and known fields
	var textParts []string

	// Common fields to extract
	textFields := []string{"prompt", "response", "message", "text", "content", "description"}
	
	for _, field := range textFields {
		if val, exists := data[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				log.Printf("Found text in field '%s': \"%s\"", field, strVal)
				textParts = append(textParts, strVal)
			}
		}
	}

	// If no specific fields found, try to serialize the entire data
	if len(textParts) == 0 {
		jsonBytes, err := json.Marshal(data)
		if err == nil {
			return string(jsonBytes)
		}
	}

	// Combine all text parts - pure semantic content only
	combinedText := strings.Join(textParts, " ")

	return combinedText
}