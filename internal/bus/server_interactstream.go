package bus

import (
	"fmt"
	"io"
	"log"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	"github.com/soaringjerry/pcas/internal/providers"
)

// InteractStream handles bidirectional streaming for real-time interactions
func (s *Server) InteractStream(stream busv1.EventBusService_InteractStreamServer) error {
	// Task 1: Get context from stream
	ctx := stream.Context()
	
	// Task 2: Handshake and config validation
	// Receive the first request which must be StreamConfig
	req, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return status.Error(codes.InvalidArgument, "stream closed before receiving config")
		}
		return status.Errorf(codes.Internal, "failed to receive initial request: %v", err)
	}
	
	// Validate that the first request is StreamConfig
	config := req.GetConfig()
	if config == nil {
		return status.Error(codes.InvalidArgument, "first request must be StreamConfig")
	}
	
	// Extract event type from config
	if config.EventType == "" {
		return status.Error(codes.InvalidArgument, "event_type cannot be empty in StreamConfig")
	}
	
	log.Printf("InteractStream: received config for event_type=%s", config.EventType)
	
	// Task 3: Routing and Provider selection
	providerName, promptTemplate := s.policyEngine.SelectProviderForStream(config.EventType)
	if providerName == "" {
		return status.Errorf(codes.NotFound, "no provider configured for event type: %s", config.EventType)
	}
	
	log.Printf("InteractStream: selected provider=%s for event_type=%s", providerName, config.EventType)
	if promptTemplate != "" {
		log.Printf("InteractStream: using prompt template: %s", promptTemplate)
	}
	
	// Get the provider instance
	provider, exists := s.providers[providerName]
	if !exists {
		return status.Errorf(codes.Internal, "provider not found: %s", providerName)
	}
	
	// Check if provider supports streaming
	streamingProvider, ok := provider.(providers.StreamingComputeProvider)
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "selected provider '%s' does not support streaming", providerName)
	}
	
	// Send ready response to client
	streamID := uuid.New().String()
	readyResp := &busv1.InteractResponse{
		ResponseType: &busv1.InteractResponse_Ready{
			Ready: &busv1.StreamReady{
				StreamId: streamID,
			},
		},
	}
	if err := stream.Send(readyResp); err != nil {
		return status.Errorf(codes.Internal, "failed to send ready response: %v", err)
	}
	
	// Task 4: Execute and data proxy
	// Create channels for bidirectional data flow
	clientStream := make(chan []byte, 10)
	serverStream := make(chan []byte, 10)
	
	// Error channel to collect errors from goroutines
	errChan := make(chan error, 2)
	
	// Start goroutine to receive data from client
	go func() {
		defer close(clientStream)
		
		for {
			req, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					log.Printf("InteractStream: client stream ended normally")
					return
				}
				errChan <- fmt.Errorf("error receiving from client: %w", err)
				return
			}
			
			// Handle different request types
			switch reqType := req.RequestType.(type) {
			case *busv1.InteractRequest_Data:
				// Forward data to provider
				if reqType.Data != nil && reqType.Data.Content != nil {
					select {
					case clientStream <- reqType.Data.Content:
						// Successfully sent
					case <-ctx.Done():
						return
					}
				}
				
			case *busv1.InteractRequest_ClientEnd:
				// Client explicitly ended the stream
				log.Printf("InteractStream: received client_end signal")
				return
				
			default:
				// Unexpected request type after config
				errChan <- fmt.Errorf("unexpected request type after config: %T", reqType)
				return
			}
		}
	}()
	
	// Start goroutine to run the provider's streaming execution
	go func() {
		defer close(serverStream)
		
		err := streamingProvider.ExecuteStream(ctx, config.Attributes, clientStream, serverStream)
		if err != nil {
			errChan <- fmt.Errorf("provider execution error: %w", err)
		}
	}()
	
	// Main goroutine: Forward data from provider to client
	for {
		select {
		case data, ok := <-serverStream:
			if !ok {
				// Server stream closed, send server_end signal
				endResp := &busv1.InteractResponse{
					ResponseType: &busv1.InteractResponse_ServerEnd{
						ServerEnd: &busv1.StreamEnd{},
					},
				}
				if err := stream.Send(endResp); err != nil {
					return status.Errorf(codes.Internal, "failed to send server_end: %v", err)
				}
				log.Printf("InteractStream: sent server_end signal")
				return nil
			}
			
			// Send data to client
			dataResp := &busv1.InteractResponse{
				ResponseType: &busv1.InteractResponse_Data{
					Data: &busv1.StreamData{
						Content: data,
					},
				},
			}
			if err := stream.Send(dataResp); err != nil {
				return status.Errorf(codes.Internal, "failed to send data: %v", err)
			}
			
		case err := <-errChan:
			// Handle errors from goroutines
			log.Printf("InteractStream: error from goroutine: %v", err)
			
			// Send error response to client
			errorResp := &busv1.InteractResponse{
				ResponseType: &busv1.InteractResponse_Error{
					Error: &busv1.StreamError{
						Code:    int32(codes.Internal),
						Message: err.Error(),
					},
				},
			}
			if sendErr := stream.Send(errorResp); sendErr != nil {
				log.Printf("InteractStream: failed to send error response: %v", sendErr)
			}
			return status.Errorf(codes.Internal, "stream error: %v", err)
			
		case <-ctx.Done():
			// Context cancelled
			return status.Error(codes.Canceled, "stream cancelled")
		}
	}
}