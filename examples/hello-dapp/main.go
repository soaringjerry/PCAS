package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/soaringjerry/pcas/pkg/sdk"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func main() {
	// Create a context that can be cancelled with Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Create PCAS client with default settings (connects to localhost:50051)
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create PCAS client: %v", err)
	}
	defer client.Close()

	log.Println("Connected to PCAS!")
	log.Println("Listening for events... (Press Ctrl+C to stop)")

	// Subscribe to all events
	eventStream, err := client.Subscribe(ctx, "hello-dapp-logger")
	if err != nil {
		log.Fatalf("Failed to subscribe to events: %v", err)
	}

	// Process events as they arrive
	for {
		select {
		case event := <-eventStream:
			if event == nil {
				log.Println("Event stream closed")
				return
			}
			printEvent(event)
		case <-ctx.Done():
			return
		}
	}
}

// printEvent formats and prints an event to the console
func printEvent(event *eventsv1.Event) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Printf("🔔 Event Received!\n")
	fmt.Printf("   ID:      %s\n", event.Id)
	fmt.Printf("   Type:    %s\n", event.Type)
	fmt.Printf("   Source:  %s\n", event.Source)
	
	if event.Subject != "" {
		fmt.Printf("   Subject: %s\n", event.Subject)
	}
	
	if event.Time != nil {
		fmt.Printf("   Time:    %s\n", event.Time.AsTime().Format(time.RFC3339))
	}
	
	if event.TraceId != "" {
		fmt.Printf("   TraceID: %s\n", event.TraceId)
	}
	
	if event.CorrelationId != "" {
		fmt.Printf("   CorrelationID: %s\n", event.CorrelationId)
	}
	
	fmt.Println(strings.Repeat("=", 60))
}