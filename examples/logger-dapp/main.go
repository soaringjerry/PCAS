package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	pcas "github.com/soaringjerry/pcas/pkg/sdk/go"
)

func main() {
	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down Logger D-App...")
		cancel()
	}()

	// Connect to PCAS server
	serverAddr := "localhost:50051"
	if addr := os.Getenv("PCAS_SERVER"); addr != "" {
		serverAddr = addr
	}

	log.Printf("Connecting to PCAS server at %s...", serverAddr)
	client, err := pcas.NewClient(ctx, serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to PCAS: %v", err)
	}
	defer client.Close()

	log.Println("Connected successfully!")

	// Subscribe to events
	eventChan, err := client.Subscribe(ctx)
	if err != nil {
		log.Fatalf("Failed to subscribe to events: %v", err)
	}
	log.Println("Subscribed to event stream")

	// Start heartbeat goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Send heartbeat event
				data := map[string]interface{}{
					"timestamp": time.Now().Unix(),
					"status":    "healthy",
					"app":       "logger-dapp",
				}

				log.Println("Sending heartbeat...")
				err := client.Emit(ctx, "dapp.heartbeat.v1", data)
				if err != nil {
					log.Printf("Failed to send heartbeat: %v", err)
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Process incoming events
	log.Println("Logger D-App is running. Press Ctrl+C to stop.")
	log.Println("========================================")

	for event := range eventChan {
		fmt.Printf("\n[%s] Event Received:\n", time.Now().Format("15:04:05"))
		fmt.Printf("  ID: %s\n", event.Id)
		fmt.Printf("  Type: %s\n", event.Type)
		fmt.Printf("  Source: %s\n", event.Source)
		
		if event.TraceId != "" {
			fmt.Printf("  Trace ID: %s\n", event.TraceId)
		}
		
		if event.CorrelationId != "" {
			fmt.Printf("  Correlation ID: %s\n", event.CorrelationId)
		}

		// Extract and print data if present
		if event.Data != nil {
			value := &structpb.Value{}
			if event.Data.MessageIs(value) {
				if err := event.Data.UnmarshalTo(value); err == nil {
					jsonBytes, _ := json.MarshalIndent(value.AsInterface(), "  ", "  ")
					fmt.Printf("  Data: %s\n", string(jsonBytes))
				}
			}
		}

		fmt.Println("----------------------------------------")
	}

	log.Println("Event stream closed")
}