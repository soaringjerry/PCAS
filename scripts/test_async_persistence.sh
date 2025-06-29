#!/bin/bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

TEST_DB="./test_async.db"
TEST_HNSW="./test_async.hnsw"
PCAS_PID=""

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up test environment...${NC}"
    if [[ -n "$PCAS_PID" ]] && kill -0 "$PCAS_PID" 2>/dev/null; then
        echo "Stopping PCAS server (PID: $PCAS_PID)..."
        kill "$PCAS_PID" || true
        wait "$PCAS_PID" 2>/dev/null || true
    fi
}

# Set trap to ensure cleanup on exit
trap cleanup EXIT

echo -e "${GREEN}=== PCAS Async Persistence Test ===${NC}"
echo "This test verifies that vectorization completes before shutdown"
echo

# Check if we have an OpenAI API key
if [[ -z "${OPENAI_API_KEY:-}" ]]; then
    echo -e "${RED}Error: OPENAI_API_KEY environment variable is not set${NC}"
    echo "This test requires OpenAI API key for embeddings"
    exit 1
fi

# Clean up any existing test data
echo -e "${YELLOW}Step 1: Cleaning up old test data...${NC}"
rm -f "$TEST_DB" "$TEST_HNSW"

# Build the binary
echo -e "${YELLOW}Step 2: Building PCAS binary...${NC}"
go build -o ./bin/pcas ./cmd/pcas

# Start PCAS server
echo -e "${YELLOW}Step 3: Starting PCAS server...${NC}"
PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" 2>&1 | tee pcas_test.log &
PCAS_PID=$!
echo "PCAS server started with PID: $PCAS_PID"

# Wait for server to be ready
echo "Waiting for server to be ready..."
for i in {1..30}; do
    if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
        echo -e "${GREEN}Server is ready!${NC}"
        break
    fi
    sleep 1
done

# Give it a moment to fully initialize
sleep 2

# Emit test event and IMMEDIATELY send shutdown signal
echo -e "${YELLOW}Step 4: Emitting event and immediately shutting down...${NC}"
./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "This is a race condition test event" --user-id "test_user"

# Wait just 1 second (not enough for vectorization to complete normally)
echo "Waiting 1 second before shutdown..."
sleep 1

# Send graceful shutdown signal
echo -e "${YELLOW}Step 5: Sending shutdown signal (SIGTERM)...${NC}"
kill -SIGTERM "$PCAS_PID"

# Wait for the process to terminate
echo "Waiting for server to shut down..."
wait "$PCAS_PID" 2>/dev/null || true
PCAS_PID=""

# Check logs for proper shutdown sequence
echo -e "${YELLOW}Step 6: Checking shutdown logs...${NC}"
if grep -q "Waiting for background vectorization to complete..." pcas_test.log; then
    echo -e "${GREEN}✅ Server waited for vectorization${NC}"
else
    echo -e "${RED}❌ Server did not wait for vectorization${NC}"
    exit 1
fi

if grep -q "All background tasks finished." pcas_test.log; then
    echo -e "${GREEN}✅ Background tasks completed${NC}"
else
    echo -e "${RED}❌ Background tasks did not complete${NC}"
    exit 1
fi

if grep -q "Successfully saved HNSW index" pcas_test.log; then
    echo -e "${GREEN}✅ HNSW index saved${NC}"
else
    echo -e "${RED}❌ HNSW index not saved${NC}"
    exit 1
fi

# Check if HNSW index file exists
if [[ -f "$TEST_HNSW" ]]; then
    echo -e "${GREEN}✅ HNSW index file created: $TEST_HNSW${NC}"
    ls -lh "$TEST_HNSW"
else
    echo -e "${RED}❌ HNSW index file not found${NC}"
    exit 1
fi

# Start server again to verify persistence
echo -e "${YELLOW}Step 7: Starting server again to verify persistence...${NC}"
PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" &
PCAS_PID=$!

# Wait for server to be ready
echo "Waiting for server to be ready..."
for i in {1..30}; do
    if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
        echo -e "${GREEN}Server is ready!${NC}"
        break
    fi
    sleep 1
done

sleep 2

# Search for the event
echo -e "${YELLOW}Step 8: Searching for the race condition test event...${NC}"
SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "test_user" "race condition test" 2>&1)
echo "$SEARCH_OUTPUT"

if echo "$SEARCH_OUTPUT" | grep -q "This is a race condition test event"; then
    echo -e "${GREEN}✅ ASYNC PERSISTENCE TEST PASSED!${NC}"
    echo -e "${GREEN}Event was properly vectorized and persisted despite immediate shutdown!${NC}"
else
    echo -e "${RED}❌ ASYNC PERSISTENCE TEST FAILED${NC}"
    echo -e "${RED}Event was lost due to race condition${NC}"
    exit 1
fi

# Clean up
echo -e "${YELLOW}Step 9: Final cleanup...${NC}"
rm -f "$TEST_DB" "$TEST_HNSW" pcas_test.log

echo -e "${GREEN}=== All async persistence tests passed successfully! ===${NC}"