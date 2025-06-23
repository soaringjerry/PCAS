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
	serverAddr := fmt.Sprintf("localhost:%s", serverPort)
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()
	
	// Create the client
	client := busv1.NewEventBusServiceClient(conn)
	
	// Create event with user-provided values
	event := &eventsv1.Event{
		Id:          uuid.New().String(),
		Source:      eventSource,
		Specversion: "1.0",
		Type:        eventType,
		Time:        timestamppb.New(time.Now()),
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
	
	// Send the event
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	resp, err := client.Publish(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to publish event: %v", err)
	}
	
	log.Printf("Event published successfully: %+v", resp)
	return nil
}

func init() {
	rootCmd.AddCommand(emitCmd)
	
	// Add flags
	emitCmd.Flags().StringVar(&eventType, "type", "", "Event type (e.g., pcas.user.login.v1)")
	emitCmd.Flags().StringVar(&eventSource, "source", "pcasctl", "Event source (default: pcasctl)")
	emitCmd.Flags().StringVar(&eventSubject, "subject", "", "Event subject (optional)")
	emitCmd.Flags().StringVar(&eventData, "data", "", "Event data as JSON string (optional)")
	emitCmd.Flags().StringVar(&serverPort, "port", "50051", "PCAS server port")
	
	// Mark type as required
	emitCmd.MarkFlagRequired("type")
}