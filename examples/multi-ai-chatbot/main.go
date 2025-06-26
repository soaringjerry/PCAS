package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func main() {
	// Parse command line flags
	var userID string
	var serverAddr string
	flag.StringVar(&userID, "user-id", "", "AI identity to chat with (required)")
	flag.StringVar(&serverAddr, "server", "localhost:50051", "PCAS server address")
	flag.Parse()

	if userID == "" {
		fmt.Println("Error: --user-id is required")
		fmt.Println("Usage: go run main.go --user-id <ai-name>")
		fmt.Println("Examples:")
		fmt.Println("  go run main.go --user-id alice   # Chat with Alice AI")
		fmt.Println("  go run main.go --user-id bob     # Chat with Bob AI")
		os.Exit(1)
	}

	// Connect to PCAS server
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to PCAS: %v", err)
	}
	defer conn.Close()

	client := busv1.NewEventBusServiceClient(conn)

	// Create a unique client ID for this session
	clientID := fmt.Sprintf("chatbot-%s-%s", userID, uuid.New().String()[:8])

	// Start event subscription in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to receive responses
	responseChan := make(chan *eventsv1.Event, 10)

	// Start subscriber goroutine
	go subscribeToResponses(ctx, client, clientID, userID, responseChan)

	// Wait a moment for subscription to establish
	time.Sleep(500 * time.Millisecond)

	fmt.Printf("🤖 Multi-AI Chatbot Started\n")
	fmt.Printf("You are now chatting with AI: %s\n", userID)
	fmt.Printf("Type 'exit' to quit or 'switch <name>' to change AI identity\n")
	fmt.Println(strings.Repeat("-", 50))

	// Create a map to track our requests
	pendingRequests := make(map[string]bool)

	// Handle incoming responses in background
	go func() {
		for event := range responseChan {
			// Extract correlation ID and response
			if correlationID := event.CorrelationId; correlationID != "" {
				if _, isPending := pendingRequests[correlationID]; isPending {
					delete(pendingRequests, correlationID)
					
					// Extract and display the response
					if event.Data != nil {
						value := &structpb.Value{}
						if event.Data.MessageIs(value) {
							if err := event.Data.UnmarshalTo(value); err == nil {
								if data, ok := value.AsInterface().(map[string]interface{}); ok {
									if response, ok := data["response"].(string); ok {
										fmt.Printf("\n🤖 %s: %s\n\n> ", userID, response)
									}
								}
							}
						}
					}
				}
			}
		}
	}()

	// Main chat loop
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())

		if input == "" {
			fmt.Print("> ")
			continue
		}

		if input == "exit" {
			fmt.Println("Goodbye!")
			break
		}

		if strings.HasPrefix(input, "switch ") {
			newUserID := strings.TrimPrefix(input, "switch ")
			if newUserID != "" {
				userID = newUserID
				fmt.Printf("\n✨ Switched to AI: %s\n\n> ", userID)
				continue
			}
		}

		// Create and send prompt event
		eventID := uuid.New().String()
		event := &eventsv1.Event{
			Id:          eventID,
			Type:        "pcas.user.prompt.v1",
			Source:      "multi-ai-chatbot",
			Specversion: "1.0",
			Time:        timestamppb.New(time.Now()),
			TraceId:     uuid.New().String(),
			UserId:      userID, // Set the AI identity
			Subject:     input,  // Use the user input as subject
		}

		// Add prompt data
		promptData := map[string]interface{}{
			"prompt": input,
		}
		if value, err := structpb.NewValue(promptData); err == nil {
			if anyData, err := anypb.New(value); err == nil {
				event.Data = anyData
			}
		}

		// Track this request
		pendingRequests[eventID] = true

		// Send the event
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := client.Publish(ctx, event)
		cancel()

		if err != nil {
			fmt.Printf("Error sending prompt: %v\n> ", err)
			delete(pendingRequests, eventID)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
	}
}

// subscribeToResponses listens for response events
func subscribeToResponses(ctx context.Context, client busv1.EventBusServiceClient, clientID, userID string, responseChan chan<- *eventsv1.Event) {
	req := &busv1.SubscribeRequest{
		ClientId: clientID,
	}

	stream, err := client.Subscribe(ctx, req)
	if err != nil {
		log.Printf("Failed to subscribe: %v", err)
		return
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled, normal shutdown
				close(responseChan)
				return
			}
			log.Printf("Stream error: %v", err)
			close(responseChan)
			return
		}

		// Filter for response events
		if event.Type == "pcas.response.v1" {
			// Check if this response is for our user
			// The response should have the original event's user ID in its metadata
			if shouldProcessResponse(event, userID) {
				responseChan <- event
			}
		}
	}
}

// shouldProcessResponse checks if a response is relevant to our user
func shouldProcessResponse(event *eventsv1.Event, userID string) bool {
	// Extract data to check the original event
	if event.Data != nil {
		value := &structpb.Value{}
		if event.Data.MessageIs(value) {
			if err := event.Data.UnmarshalTo(value); err == nil {
				if data, ok := value.AsInterface().(map[string]interface{}); ok {
					// The server includes the original event ID in the response
					// We use correlation ID to match request/response pairs
					return true // We'll filter by correlation ID in the main loop
				}
			}
		}
	}
	return false
}