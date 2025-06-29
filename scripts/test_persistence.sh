#!/bin/bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

TEST_DB="./test_persistence.db"
TEST_HNSW="./test_persistence.hnsw"
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

echo -e "${GREEN}=== PCAS Persistence Test ===${NC}"
echo "This test verifies that HNSW index is properly persisted across restarts"
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

# Start PCAS server (first run)
echo -e "${YELLOW}Step 3: Starting PCAS server (first run)...${NC}"
PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" &
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

# Emit test events
echo -e "${YELLOW}Step 4: Emitting test events...${NC}"
./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "The quick brown fox jumps over the lazy dog" --user-id "test_user"
./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "Python is a great programming language for data science" --user-id "test_user"
./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "Machine learning algorithms can detect patterns in data" --user-id "test_user"

# Give time for vectorization
echo "Waiting for events to be vectorized..."
sleep 5

# Search to ensure events are indexed
echo -e "${YELLOW}Step 5: Searching to verify initial indexing...${NC}"
SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "test_user" "programming language" 2>&1)
echo "$SEARCH_OUTPUT"

if echo "$SEARCH_OUTPUT" | grep -q "Python"; then
    echo -e "${GREEN}✅ Initial search successful${NC}"
else
    echo -e "${RED}❌ Initial search failed${NC}"
    exit 1
fi

# Gracefully stop the server
echo -e "${YELLOW}Step 6: Gracefully stopping server (Ctrl+C simulation)...${NC}"
kill -SIGTERM "$PCAS_PID"
wait "$PCAS_PID" 2>/dev/null || true
PCAS_PID=""
echo "Server stopped"

# Check if HNSW index was saved
if [[ -f "$TEST_HNSW" ]]; then
    echo -e "${GREEN}✅ HNSW index file created: $TEST_HNSW${NC}"
    ls -lh "$TEST_HNSW"
else
    echo -e "${RED}❌ HNSW index file not found${NC}"
    exit 1
fi

# Start PCAS server again (second run)
echo -e "${YELLOW}Step 7: Starting PCAS server again (second run)...${NC}"
PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" &
PCAS_PID=$!
echo "PCAS server restarted with PID: $PCAS_PID"

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

# Search again to verify persistence
echo -e "${YELLOW}Step 8: Searching to verify persistence...${NC}"
SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "test_user" "programming language" 2>&1)
echo "$SEARCH_OUTPUT"

if echo "$SEARCH_OUTPUT" | grep -q "Python"; then
    echo -e "${GREEN}✅ PERSISTENCE TEST PASSED: Events are still searchable after restart!${NC}"
else
    echo -e "${RED}❌ PERSISTENCE TEST FAILED: Events not found after restart${NC}"
    exit 1
fi

# Clean up
echo -e "${YELLOW}Step 9: Final cleanup...${NC}"
rm -f "$TEST_DB" "$TEST_HNSW"

echo -e "${GREEN}=== All persistence tests passed successfully! ===${NC}"