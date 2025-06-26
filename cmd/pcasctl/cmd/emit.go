package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

var (
	eventType   string
	eventSource string
	eventSubject string
	eventData   string
	serverPort  string
	serverAddr  string // Full server address (host:port)
	traceID     string // Optional trace ID for correlation
)

var emitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Emit an event to the PCAS bus",
	Long: `Emit an event to the PCAS bus for processing by the decision-making 
engine. Events can trigger actions, update context, or initiate workflows.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return emitEvent()
	},
}

func emitEvent() error {
	// Connect to the PCAS server
	if serverAddr == "" {
		serverAddr = fmt.Sprintf("localhost:%s", serverPort)
	}
	
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()
	
	// Create the client
	client := busv1.NewEventBusServiceClient(conn)
	
	// Generate a unique client ID
	clientID := fmt.Sprintf("pcasctl-%s", uuid.New().String()[:8])
	
	// Start subscription in background goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Channel to signal when subscription is ready
	subReady := make(chan bool)
	
	// Start subscriber goroutine
	go func() {
		subscribeToEvents(ctx, client, clientID, subReady)
	}()
	
	// Wait for subscription to be ready
	select {
	case <-subReady:
		log.Println("Subscription established, emitting event...")
	case <-time.After(2 * time.Second):
		log.Println("Warning: Subscription setup timed out, continuing anyway...")
	}
	
	// Create event with user-provided values
	event := &eventsv1.Event{
		Id:          uuid.New().String(),
		Source:      eventSource,
		Specversion: "1.0",
		Type:        eventType,
		Time:        timestamppb.New(time.Now()),
	}
	
	// Set trace ID - use provided one or generate new
	if traceID != "" {
		event.TraceId = traceID
	} else {
		event.TraceId = uuid.New().String()
	}
	
	// Add subject if provided
	if eventSubject != "" {
		event.Subject = eventSubject
	}
	
	// Parse and add data if provided
	if eventData != "" {
		var jsonData map[string]interface{}
		if err := json.Unmarshal([]byte(eventData), &jsonData); err != nil {
			return fmt.Errorf("failed to parse --data JSON: %v", err)
		}
		
		// Convert to structpb.Value
		value, err := structpb.NewValue(jsonData)
		if err != nil {
			return fmt.Errorf("failed to convert data to protobuf Value: %v", err)
		}
		
		// Wrap in Any
		anyData, err := anypb.New(value)
		if err != nil {
			return fmt.Errorf("failed to wrap data in Any: %v", err)
		}
		
		event.Data = anyData
	}
	
	// Send the event with extended timeout for RAG-enhanced processing
	pubCtx, pubCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer pubCancel()
	
	resp, err := client.Publish(pubCtx, event)
	if err != nil {
		return fmt.Errorf("failed to publish event: %v", err)
	}
	
	log.Printf("Event published successfully: %+v", resp)
	
	// Wait a bit to receive responses
	log.Println("Waiting for responses...")
	time.Sleep(3 * time.Second)
	
	return nil
}

// subscribeToEvents handles the subscription stream
func subscribeToEvents(ctx context.Context, client busv1.EventBusServiceClient, clientID string, ready chan<- bool) {
	// Create subscription request
	req := &busv1.SubscribeRequest{
		ClientId: clientID,
	}
	
	// Start subscription stream
	stream, err := client.Subscribe(ctx, req)
	if err != nil {
		log.Printf("Failed to subscribe: %v", err)
		close(ready)
		return
	}
	
	// Signal that subscription is ready
	close(ready)
	
	// Receive events
	for {
		event, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled, normal shutdown
				return
			}
			log.Printf("Stream error: %v", err)
			return
		}
		
		// Print received event
		log.Printf("\n=== Received Event ===")
		log.Printf("ID: %s", event.Id)
		log.Printf("Type: %s", event.Type)
		log.Printf("Source: %s", event.Source)
		log.Printf("Subject: %s", event.Subject)
		
		// Extract and print data if present
		if event.Data != nil {
			value := &structpb.Value{}
			if event.Data.MessageIs(value) {
				if err := event.Data.UnmarshalTo(value); err == nil {
					jsonBytes, _ := json.MarshalIndent(value.AsInterface(), "", "  ")
					log.Printf("Data: %s", string(jsonBytes))
				}
			}
		}
		log.Printf("====================\n")
	}
}

func init() {
	rootCmd.AddCommand(emitCmd)
	
	// Add flags
	emitCmd.Flags().StringVarP(&eventType, "type", "t", "", "Event type (e.g., pcas.user.login.v1)")
	emitCmd.Flags().StringVar(&eventSource, "source", "pcasctl", "Event source (default: pcasctl)")
	emitCmd.Flags().StringVar(&eventSubject, "subject", "", "Event subject (optional)")
	emitCmd.Flags().StringVar(&eventData, "data", "", "Event data as JSON string (optional)")
	emitCmd.Flags().StringVar(&serverPort, "port", "50051", "PCAS server port")
	emitCmd.Flags().StringVar(&serverAddr, "server", "", "PCAS server address (overrides --port)")
	emitCmd.Flags().StringVar(&traceID, "trace-id", "", "Trace ID for correlation (optional, auto-generated if not provided)")
	
	// Mark type as required
	emitCmd.MarkFlagRequired("type")
}