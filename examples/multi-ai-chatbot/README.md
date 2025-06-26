# Multi-AI Chatbot Example

This example demonstrates how to build a DApp that leverages PCAS's multi-identity capabilities. Each AI identity has its own memory and context, providing personalized responses based on their interaction history.

## Features

- Chat with different AI identities using `--user-id`
- Each AI maintains its own memory and context through PCAS
- Switch between AI identities during runtime
- Demonstrates user-specific RAG (Retrieval Augmented Generation)

## Prerequisites

1. PCAS server running with RAG enabled:
   ```bash
   cd /path/to/pcas
   make dev-up
   ```

2. Configure your `.env` file with OpenAI API key:
   ```bash
   OPENAI_API_KEY=your-api-key-here
   ```

## Running the Example

1. Start chatting with Alice AI:
   ```bash
   go run main.go --user-id alice
   ```

2. In another terminal, chat with Bob AI:
   ```bash
   go run main.go --user-id bob
   ```

## How It Works

1. **User Identity**: Each instance of the chatbot uses a unique `user_id` to identify which AI you're talking to.

2. **Event Publishing**: When you type a message, it's sent as a `pcas.user.prompt.v1` event with the `user_id` field set.

3. **Memory Isolation**: PCAS's hybrid search system ensures that each AI only retrieves memories from their own previous conversations.

4. **Personalized Responses**: The RAG system enhances prompts with user-specific context, making each AI's responses unique to their conversation history.

## Commands

- Type any message to chat with the current AI
- `switch <name>` - Switch to a different AI identity
- `exit` - Quit the chatbot

## Example Session

```
$ go run main.go --user-id alice
🤖 Multi-AI Chatbot Started
You are now chatting with AI: alice
Type 'exit' to quit or 'switch <name>' to change AI identity
--------------------------------------------------
> Hi! My favorite color is blue.

🤖 alice: Nice to meet you! I'll remember that your favorite color is blue. Is there anything else you'd like to share about yourself?

> What's my favorite color?

🤖 alice: Your favorite color is blue! You just told me that a moment ago.

> switch bob
✨ Switched to AI: bob

> What's my favorite color?

🤖 bob: I don't have any information about your favorite color. Would you like to tell me?
```

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Chatbot    │     │    PCAS     │     │  Vector DB  │
│ (user=alice)│────▶│   Server    │────▶│ (PostgreSQL)│
└─────────────┘     └─────────────┘     └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │   OpenAI    │
                    │   GPT-4     │
                    └─────────────┘
```

Each chatbot instance maintains its own identity, and PCAS ensures memory isolation through user-specific vector filtering.