---
title: "Hello D-App Tutorial"
description: "Learn how to build your first Decentralized Application (D-App) that connects to PCAS and receives real-time events."
tags: ["tutorial", "dapp", "getting-started", "go"]
version: "0.1.2"
---

# Hello D-App Tutorial

Welcome to the PCAS ecosystem! This tutorial will guide you through building your first D-App (Decentralized Application) that connects to PCAS and receives events in real-time.

## What You'll Build

You'll create an **Intelligent Console D-App** that:
- Connects to a running PCAS instance
- Asks questions to PCAS
- Receives intelligent answers powered by AI
- Demonstrates the complete "dApp asks → PCAS thinks → dApp receives answer" flow

This is the perfect starting point for understanding how D-Apps leverage PCAS's AI capabilities.

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

This code creates an Intelligent Console D-App that:
1. Connects to PCAS on the default port (50051)
2. Subscribes to the event stream with a unique client ID
3. Displays both your questions and PCAS's AI-powered responses
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

### 3. Understanding Policy Routing

Before we ask our first question, let's understand how PCAS routes events to AI providers. Open the `policy.yaml` file in the PCAS directory:

```yaml
# Look for this rule in policy.yaml
- name: "Rule for user prompts"
  if:
    event_type: "pcas.user.prompt.v1"
  then:
    provider: openai-gpt4
```

This rule tells PCAS: "When you receive an event of type `pcas.user.prompt.v1`, route it to the OpenAI GPT-4 provider for processing." This is how your questions get "thought about" rather than just stored.

### 4. Ask PCAS a Question

Now, let's ask PCAS a question! In a third terminal, emit a prompt event:

```bash
# In the PCAS directory
./bin/pcasctl emit --type "pcas.user.prompt.v1" --data '{"text": "What is the capital of France?"}'
```

This command sends a question to PCAS, which will:
1. Receive the event
2. Route it to the AI provider (according to the policy)
3. Generate an intelligent response
4. Send the response back as a new event

### 5. Observe the AI Conversation

Switch back to your D-App terminal. You should see two events:

1. **Your question event** (`pcas.user.prompt.v1`)
2. **The AI response event** (`pcas.response.v1`) containing the answer

Example output:
```
============================================================
🔔 Event Received!
   ID:      prompt-123...
   Type:    pcas.user.prompt.v1
   Source:  pcasctl
   Time:    2024-06-25T12:00:00Z
============================================================

============================================================
🔔 Event Received!
   ID:      response-456...
   Type:    pcas.response.v1
   Source:  pcas-server
   Time:    2024-06-25T12:00:01Z
   CorrelationID: prompt-123...
============================================================
```

The response event contains the AI's answer in its data payload. In this case, you would see "Paris" as the answer to your question about France's capital.

## Bonus: Searching Your Memory

All your interactions with PCAS are automatically stored in its memory. You can search through these memories using semantic search. Let's try searching for the conversation we just had:

```bash
# In the PCAS directory
./bin/pcasctl search "questions about France"
```

You should see results that include:
- Your original question event
- The AI's response event
- Relevance scores showing how closely each event matches your search query

This demonstrates PCAS's powerful memory system - every interaction is remembered and can be intelligently retrieved later!

## Congratulations!

You've successfully built your first Intelligent Console D-App! This demonstrates the core PCAS pattern:

1. **Connect** to PCAS
2. **Ask** questions by emitting prompt events
3. **Receive** AI-powered responses
4. **Search** through your conversation history

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

This tutorial introduced you to intelligent D-App development with PCAS. You learned how to:
- Set up a new D-App project
- Connect to PCAS using the SDK
- Ask questions and receive AI-powered responses
- Search through your conversation history
- Understand how PCAS routes events to AI providers

The Intelligent Console pattern you've learned here is the foundation for building sophisticated AI-powered applications that leverage PCAS's memory and reasoning capabilities.

Happy coding! 🚀