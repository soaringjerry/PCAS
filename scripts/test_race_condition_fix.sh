#!/bin/bash
set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

TEST_DB="./test_race.db"
TEST_HNSW="./test_race.hnsw"
PCAS_PID=""
LOG_FILE="pcas_race_test.log"

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

echo -e "${GREEN}=== PCAS Race Condition Fix Test ===${NC}"
echo "This test verifies that the shutdown race condition is fixed"
echo

# Check if we have an OpenAI API key
if [[ -z "${OPENAI_API_KEY:-}" ]]; then
    echo -e "${RED}Error: OPENAI_API_KEY environment variable is not set${NC}"
    echo "This test requires OpenAI API key for embeddings"
    exit 1
fi

# Clean up any existing test data
echo -e "${YELLOW}Step 1: Cleaning up old test data...${NC}"
rm -f "$TEST_DB" "$TEST_HNSW" "$LOG_FILE"

# Build the binary
echo -e "${YELLOW}Step 2: Building PCAS binary...${NC}"
go build -o ./bin/pcas ./cmd/pcas

# Test multiple times to ensure reliability
for i in {1..3}; do
    echo
    echo -e "${YELLOW}=== Test Run $i/3 ===${NC}"
    
    # Clean up from previous run
    rm -f "$TEST_DB" "$TEST_HNSW"
    
    # Start PCAS server
    echo "Starting PCAS server..."
    PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" 2>&1 | tee "$LOG_FILE" &
    PCAS_PID=$!
    
    # Wait for server to be ready
    echo "Waiting for server to be ready..."
    for j in {1..30}; do
        if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
            echo -e "${GREEN}Server is ready!${NC}"
            break
        fi
        sleep 1
    done
    
    sleep 2
    
    # Emit test event
    echo "Emitting test event..."
    ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" \
        --subject "Race condition test run $i" --user-id "test_user"
    
    # Very short wait (to stress test the race condition)
    sleep 0.5
    
    # Send shutdown signal
    echo "Sending shutdown signal..."
    kill -SIGTERM "$PCAS_PID"
    
    # Wait for process to exit
    echo "Waiting for graceful shutdown..."
    wait "$PCAS_PID" 2>/dev/null || true
    PCAS_PID=""
    
    # Verify proper shutdown sequence in logs
    echo "Checking shutdown sequence..."
    if ! grep -q "Graceful shutdown complete" "$LOG_FILE"; then
        echo -e "${RED}❌ Graceful shutdown did not complete properly${NC}"
        cat "$LOG_FILE"
        exit 1
    fi
    
    if ! grep -q "Server exited gracefully" "$LOG_FILE"; then
        echo -e "${RED}❌ Server did not exit gracefully${NC}"
        cat "$LOG_FILE"
        exit 1
    fi
    
    # Check if HNSW index was saved
    if [[ ! -f "$TEST_HNSW" ]]; then
        echo -e "${RED}❌ HNSW index file not found${NC}"
        exit 1
    fi
    
    # Start server again
    echo "Starting server again to verify persistence..."
    PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" &
    PCAS_PID=$!
    
    # Wait for server to be ready
    for j in {1..30}; do
        if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
            break
        fi
        sleep 1
    done
    
    sleep 2
    
    # Search for the event
    echo "Searching for test event..."
    SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "test_user" "test run $i" 2>&1)
    
    if echo "$SEARCH_OUTPUT" | grep -q "Race condition test run $i"; then
        echo -e "${GREEN}✅ Test run $i PASSED: Event persisted correctly${NC}"
    else
        echo -e "${RED}❌ Test run $i FAILED: Event not found${NC}"
        echo "Search output: $SEARCH_OUTPUT"
        exit 1
    fi
    
    # Stop server before next iteration
    kill -SIGTERM "$PCAS_PID"
    wait "$PCAS_PID" 2>/dev/null || true
    PCAS_PID=""
done

# Clean up
echo
echo -e "${YELLOW}Final cleanup...${NC}"
rm -f "$TEST_DB" "$TEST_HNSW" "$LOG_FILE"

echo
echo -e "${GREEN}=== ALL RACE CONDITION TESTS PASSED! ===${NC}"
echo "The shutdown race condition has been successfully fixed!"
echo "The server now waits for all shutdown procedures to complete before exiting."