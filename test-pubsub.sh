#!/bin/bash

# Test script for Pub/Sub functionality

# Check if OPENAI_API_KEY is set for full test
if [ -z "$OPENAI_API_KEY" ]; then
    echo "Note: OPENAI_API_KEY not set, will test with mock provider only"
fi

# Start the server in the background
echo "Starting PCAS server..."
./bin/pcas serve --port 9093 > server.log 2>&1 &
SERVER_PID=$!

# Give the server time to start
sleep 2

echo "========================================="
echo "Test 1: Mock provider (should work without API key)"
echo "========================================="
./bin/pcasctl emit --type pcas.test.v1 --data '{"action": "test", "message": "Testing pub/sub"}' --port 9093

echo ""
echo "========================================="
echo "Test 2: OpenAI provider (requires API key)"  
echo "========================================="
if [ -n "$OPENAI_API_KEY" ]; then
    ./bin/pcasctl emit --type pcas.user.prompt.v1 --data '{"prompt": "Hello! Reply with a simple greeting in 5 words or less."}' --port 9093
else
    echo "Skipping OpenAI test (no API key)"
fi

# Kill the server
echo ""
echo "Stopping server..."
kill $SERVER_PID 2>/dev/null

echo "Test complete!"
echo "Server log saved to server.log"