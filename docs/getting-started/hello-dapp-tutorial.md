---
title: "Hello D-App Tutorial"
description: "Learn how to build your first Decentralized Application (D-App) that connects to PCAS and receives real-time events."
tags: ["tutorial", "dapp", "getting-started", "go"]
version: "0.1.2"
---

# Hello D-App Tutorial

Welcome to the PCAS ecosystem! This tutorial will guide you through building your first D-App (Decentralized Application) that connects to PCAS and receives events in real-time.

## What You'll Build

You'll create a simple "Logger D-App" that:
- Connects to a running PCAS instance
- Subscribes to all events
- Prints received events to the console

This is the perfect starting point for understanding how D-Apps interact with PCAS.

## Prerequisites

- Go 1.21 or later installed
- PCAS running locally (we'll show you how)
- Basic familiarity with Go programming

## Step 1: Initialize Your Project

Create a new directory for your D-App and initialize a Go module:

```bash
mkdir hello-dapp
cd hello-dapp
go mod init hello-dapp
```

## Step 2: Add PCAS SDK Dependency

Add the PCAS SDK to your project:

```bash
go get github.com/soaringjerry/pcas/pkg/sdk
```

## Step 3: Write Your D-App Code

Create a file named `main.go` with the following content:

```go
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
```

This code creates a simple D-App that:
1. Connects to PCAS on the default port (50051)
2. Subscribes to the event stream with a unique client ID
3. Prints each received event in a readable format
4. Handles graceful shutdown with Ctrl+C

## Step 4: Run and Verify

### 1. Start PCAS

First, ensure PCAS is running with all required services:

```bash
# In the PCAS directory
make dev-up
```

Wait for the services to be ready. You should see logs indicating ChromaDB and PCAS are running.

### 2. Start Your D-App

In a new terminal, run your D-App:

```bash
# In your hello-dapp directory
go run main.go
```

You should see:
```
Connected to PCAS!
Listening for events... (Press Ctrl+C to stop)
```

### 3. Emit a Test Event

In a third terminal, emit an event using pcasctl:

```bash
# In the PCAS directory
./bin/pcasctl emit --type "pcas.memory.create.v1" --subject "Hello from my first D-App test!"
```

### 4. Observe the Results

Switch back to your D-App terminal. You should see two events:

1. **The original event** you emitted
2. **A response event** from PCAS after processing

Example output:
```
============================================================
🔔 Event Received!
   ID:      abc123...
   Type:    pcas.memory.create.v1
   Source:  pcasctl
   Subject: Hello from my first D-App test!
   Time:    2024-06-25T12:00:00Z
============================================================

============================================================
🔔 Event Received!
   ID:      def456...
   Type:    pcas.response.v1
   Source:  pcas-server
   Subject: response-to-abc123...
   Time:    2024-06-25T12:00:01Z
   CorrelationID: abc123...
============================================================
```

## Congratulations!

You've successfully built your first D-App! This Logger D-App demonstrates the fundamental pattern for all PCAS D-Apps:

1. **Connect** to PCAS
2. **Subscribe** to events
3. **Process** events as they arrive

## Next Steps

Now that you understand the basics, you can:

1. **Filter Events**: Modify the subscription to only receive specific event types
2. **Emit Events**: Have your D-App emit its own events back to PCAS
3. **Add Business Logic**: Process events and trigger actions based on their content
4. **Build Real Applications**: Create D-Apps for scheduling, automation, monitoring, and more
5. **Build Multi-Identity Applications**: Create apps that support multiple AI personalities with isolated memories

## Building a Multi-Identity Application

PCAS now supports multi-identity applications through the `user_id` field. This enables you to build applications where different users or AI personalities have their own isolated memories and contexts.

### Example: Multi-AI Chatbot

We've created a comprehensive example that demonstrates this capability. The Multi-AI Chatbot allows you to:

- Chat with different AI identities (e.g., "alice", "bob")
- Each AI maintains its own memory through PCAS
- Switch between AIs dynamically during a session
- Experience how each AI's responses are personalized based on their unique conversation history

Check out the complete example: [`examples/multi-ai-chatbot`](../examples/multi-ai-chatbot/)

### Key Concepts

**User Identity (`user_id`)**: Every event can include a `user_id` field that identifies which user or AI persona the event belongs to. PCAS uses this for:
- Memory isolation in vector storage
- User-specific RAG (Retrieval Augmented Generation)
- Personalized context retrieval

**Usage Example**:
```go
event := &eventsv1.Event{
    Type:    "pcas.user.prompt.v1",
    UserId:  "alice",  // This event belongs to the "alice" AI
    Subject: "What's the weather like?",
    // ... other fields
}
```

When PCAS processes this event with RAG enabled, it will only retrieve memories that belong to "alice", ensuring each AI maintains its own unique personality and knowledge.

## Example: Filtering Events

To subscribe to only specific event types, modify the Subscribe call:

```go
// Subscribe to only memory events
eventStream, err := client.Subscribe(ctx, "hello-dapp-logger", 
    sdk.WithEventTypes("pcas.memory.create.v1", "pcas.memory.update.v1"))
```

## Troubleshooting

**Connection Failed**: Ensure PCAS is running on localhost:50051
```bash
# Check if PCAS is responding
./bin/pcasctl ping
```

**No Events Received**: Verify your D-App successfully subscribed by checking PCAS logs for "Client hello-dapp-logger subscribed"

**Import Errors**: Run `go mod tidy` to ensure all dependencies are properly resolved

## Summary

This tutorial introduced you to D-App development with PCAS. You learned how to:
- Set up a new D-App project
- Connect to PCAS using the SDK
- Subscribe to and process events
- Handle graceful shutdown

The Logger D-App pattern you've learned here is the foundation for building more complex D-Apps that can transform how you interact with your personal computing environment.

Happy coding! 🚀