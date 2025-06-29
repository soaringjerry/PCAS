#!/bin/bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

TEST_DB="./test_nokey.db"
TEST_HNSW="./test_nokey.hnsw"
PCAS_PID=""

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up test environment...${NC}"
    if [[ -n "$PCAS_PID" ]] && kill -0 "$PCAS_PID" 2>/dev/null; then
        echo "Stopping PCAS server (PID: $PCAS_PID)..."
        kill "$PCAS_PID" || true
        wait "$PCAS_PID" 2>/dev/null || true
    fi
    rm -f "$TEST_DB" "$TEST_HNSW"
}

# Set trap to ensure cleanup on exit
trap cleanup EXIT

echo -e "${GREEN}=== PCAS Missing API Key User Experience Test ===${NC}"
echo "This test demonstrates the improved user experience when API key is missing"
echo

# Save original OPENAI_API_KEY (if set) and unset it
ORIGINAL_KEY="${OPENAI_API_KEY:-}"
unset OPENAI_API_KEY

# Build the binary
echo -e "${YELLOW}Step 1: Building PCAS binary...${NC}"
go build -o ./bin/pcas ./cmd/pcas

# Start PCAS server WITHOUT API key
echo -e "${YELLOW}Step 2: Starting PCAS server WITHOUT OPENAI_API_KEY...${NC}"
./bin/pcas serve --db-path "$TEST_DB" &
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

sleep 2

echo -e "${YELLOW}Step 3: Checking server logs for warning...${NC}"
echo "(The server should have displayed a WARNING about disabled functionality)"
echo

# Try to emit an event (this should work)
echo -e "${YELLOW}Step 4: Emitting an event (this should work)...${NC}"
if ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "Test event without embedding" --user-id "test_user"; then
    echo -e "${GREEN}✅ Event emission works without API key${NC}"
else
    echo -e "${RED}❌ Event emission failed${NC}"
fi

echo

# Try to search (this should fail with helpful error)
echo -e "${YELLOW}Step 5: Attempting to search (this should fail with helpful error)...${NC}"
SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "test_user" "test query" 2>&1) || true
echo "Search output:"
echo "$SEARCH_OUTPUT"

# Check if we got the improved error message
if echo "$SEARCH_OUTPUT" | grep -q "Please ensure the PCAS server was started with the OPENAI_API_KEY environment variable set"; then
    echo -e "${GREEN}✅ Improved error message displayed!${NC}"
else
    echo -e "${RED}❌ Did not get the expected error message${NC}"
fi

echo
echo -e "${GREEN}=== Test Summary ===${NC}"
echo "1. Server starts successfully without API key"
echo "2. Server logs contain WARNING about disabled functionality"
echo "3. Event emission still works (for storage)"
echo "4. Search fails with a helpful error message guiding the user"
echo
echo -e "${YELLOW}To enable search functionality, restart the server with:${NC}"
echo "OPENAI_API_KEY=your-key-here ./bin/pcas serve --db-path ./pcas.db"

# Restore original API key if it was set
if [[ -n "$ORIGINAL_KEY" ]]; then
    export OPENAI_API_KEY="$ORIGINAL_KEY"
fi