#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
MAX_WAIT_TIME=60  # Maximum time to wait for services to be ready (seconds)
WAIT_INTERVAL=2   # Interval between health checks (seconds)
TEST_EVENT_TEXT="The quickstart validation event."
SEARCH_QUERY="Which event is for validation?"

# Default mode is SQLite
MODE="sqlite"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --mode)
            MODE="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--mode sqlite|docker]"
            exit 1
            ;;
    esac
done

# Validate mode
if [[ "$MODE" != "sqlite" && "$MODE" != "docker" ]]; then
    echo -e "${RED}Invalid mode: $MODE. Must be 'sqlite' or 'docker'${NC}"
    exit 1
fi

echo -e "${GREEN}=== PCAS Quick Start Validation Script ===${NC}"
echo "Mode: $MODE"
echo

# SQLite mode functions
run_sqlite_tests() {
    local PCAS_PID=""
    local TEST_DB="./test.db"
    local TEST_HNSW="./test.hnsw"
    
    # Cleanup function for SQLite mode
    cleanup_sqlite() {
        echo -e "${YELLOW}Cleaning up SQLite test environment...${NC}"
        if [[ -n "$PCAS_PID" ]] && kill -0 "$PCAS_PID" 2>/dev/null; then
            echo "Stopping PCAS server (PID: $PCAS_PID)..."
            kill "$PCAS_PID" || true
            wait "$PCAS_PID" 2>/dev/null || true
        fi
        rm -f "$TEST_DB" "$TEST_HNSW"
    }
    
    # Set trap to ensure cleanup on exit
    trap cleanup_sqlite EXIT
    
    # Step 1: Build the binary
    echo -e "${YELLOW}Step 1: Building PCAS binary...${NC}"
    if ! go build -o ./bin/pcas ./cmd/pcas; then
        echo -e "${RED}Failed to build PCAS binary${NC}"
        exit 1
    fi
    echo -e "${GREEN}Binary built successfully${NC}"
    
    # Step 2: Clean up old test data
    echo -e "${YELLOW}Step 2: Cleaning up old test data...${NC}"
    rm -f "$TEST_DB" "$TEST_HNSW"
    
    # Step 3: Start PCAS server
    echo -e "${YELLOW}Step 3: Starting PCAS server...${NC}"
    PCAS_RAG_ENABLED=true ./bin/pcas serve --db-path "$TEST_DB" &
    PCAS_PID=$!
    echo "PCAS server started with PID: $PCAS_PID"
    
    # Step 4: Wait for server to be ready
    echo -e "${YELLOW}Step 4: Waiting for PCAS server to be ready...${NC}"
    local elapsed=0
    while [ $elapsed -lt $MAX_WAIT_TIME ]; do
        if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
            echo -e "${GREEN}PCAS server is ready!${NC}"
            break
        fi
        sleep $WAIT_INTERVAL
        elapsed=$((elapsed + WAIT_INTERVAL))
        echo -n "."
    done
    
    if [ $elapsed -ge $MAX_WAIT_TIME ]; then
        echo -e "\n${RED}Timeout waiting for PCAS server to be ready${NC}"
        exit 1
    fi
    
    # Give it a bit more time to fully initialize
    sleep 2
    
    # Step 5: Test multi-user event storage
    echo -e "${YELLOW}Step 5: Testing multi-user event storage...${NC}"
    
    # User A writes about dogs
    echo "User A writing about dogs..."
    if ! ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "我喜欢狗狗" --user-id "user_A"; then
        echo -e "${RED}Failed to emit user A's dog event${NC}"
        exit 1
    fi
    
    # User B writes about cats
    echo "User B writing about cats..."
    if ! ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "我喜欢猫猫" --user-id "user_B"; then
        echo -e "${RED}Failed to emit user B's cat event${NC}"
        exit 1
    fi
    
    # Also emit the standard test event for backwards compatibility
    echo "Emitting standard test event..."
    if ! ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "${TEST_EVENT_TEXT}"; then
        echo -e "${RED}Failed to emit standard test event${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}All events emitted successfully${NC}"
    
    # Give the system time to process and index events
    sleep 3
    
    # Step 6: Verify database contents
    echo -e "${YELLOW}Step 6: Verifying database contents...${NC}"
    
    # Check nodes table
    echo "Checking nodes table..."
    NODE_COUNT=$(sqlite3 "$TEST_DB" "SELECT COUNT(*) FROM nodes WHERE type = 'event';")
    echo "Event nodes in database: $NODE_COUNT"
    
    if [ "$NODE_COUNT" -lt 3 ]; then
        echo -e "${RED}Expected at least 3 event nodes, found $NODE_COUNT${NC}"
        exit 1
    fi
    
    # Check if user IDs are stored correctly
    USER_A_COUNT=$(sqlite3 "$TEST_DB" "SELECT COUNT(*) FROM nodes WHERE type = 'event' AND json_extract(content, '$.user_id') = 'user_A';")
    USER_B_COUNT=$(sqlite3 "$TEST_DB" "SELECT COUNT(*) FROM nodes WHERE type = 'event' AND json_extract(content, '$.user_id') = 'user_B';")
    
    echo "User A events: $USER_A_COUNT"
    echo "User B events: $USER_B_COUNT"
    
    if [ "$USER_A_COUNT" -eq 0 ] || [ "$USER_B_COUNT" -eq 0 ]; then
        echo -e "${RED}User-specific events not found in database${NC}"
        exit 1
    fi
    
    # Check if embeddings are enabled
    OPENAI_API_KEY="${OPENAI_API_KEY:-}"
    if [[ -n "$OPENAI_API_KEY" ]] && [[ "$OPENAI_API_KEY" != "dummy-key-for-testing" ]]; then
        # Step 7: Test user-filtered search
        echo -e "${YELLOW}Step 7: Testing user-filtered search...${NC}"
        
        # Search as User A for "狗狗" (dogs)
        echo "User A searching for '狗狗'..."
        SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" --user-id "user_A" "狗狗" 2>&1) || {
            echo -e "${RED}Search command failed${NC}"
            echo "Output: ${SEARCH_OUTPUT}"
            exit 1
        }
        
        echo "Search output:"
        echo "${SEARCH_OUTPUT}"
        
        # Verify User A sees their dog event
        if echo "${SEARCH_OUTPUT}" | grep -q "我喜欢狗狗"; then
            echo -e "${GREEN}✅ User A correctly found their dog event${NC}"
        else
            echo -e "${RED}❌ User A's dog event not found${NC}"
            exit 1
        fi
        
        # Verify User A does NOT see User B's cat event
        if echo "${SEARCH_OUTPUT}" | grep -q "我喜欢猫猫"; then
            echo -e "${RED}❌ SECURITY VIOLATION: User A can see User B's cat event!${NC}"
            exit 1
        else
            echo -e "${GREEN}✅ User isolation working: User A cannot see User B's events${NC}"
        fi
        
        # Also test the standard search
        echo "Testing standard search..."
        STANDARD_SEARCH=$(./bin/pcasctl search --server "127.0.0.1:50051" "${SEARCH_QUERY}" 2>&1) || {
            echo -e "${YELLOW}Standard search skipped${NC}"
        }
        
        if echo "${STANDARD_SEARCH}" | grep -q "${TEST_EVENT_TEXT}"; then
            echo -e "${GREEN}✅ Standard search working correctly${NC}"
        fi
    else
        echo -e "${YELLOW}Skipping search tests - no valid OpenAI API key available${NC}"
        echo -e "${GREEN}Note: Full validation requires a valid OpenAI API key for search functionality${NC}"
    fi
    
    echo -e "${GREEN}✅ SQLite mode validation PASSED${NC}"
}

# Docker mode functions (original implementation)
run_docker_tests() {
    # Get OpenAI API key from environment
    OPENAI_API_KEY="${OPENAI_API_KEY:-}"
    
    # Cleanup function
    cleanup() {
        echo -e "${YELLOW}Cleaning up Docker environment...${NC}"
        make dev-down || true
    }
    
    # Set trap to ensure cleanup on exit
    trap cleanup EXIT
    
    # Function to check if a service is healthy
    check_service_health() {
        local service_name=$1
        local health_check_cmd=$2
        local elapsed=0
        
        echo -e "${YELLOW}Waiting for ${service_name} to be ready...${NC}"
        
        while [ $elapsed -lt $MAX_WAIT_TIME ]; do
            if eval "$health_check_cmd" >/dev/null 2>&1; then
                echo -e "${GREEN}${service_name} is ready!${NC}"
                return 0
            fi
            
            sleep $WAIT_INTERVAL
            elapsed=$((elapsed + WAIT_INTERVAL))
            echo -n "."
        done
        
        echo -e "\n${RED}Timeout waiting for ${service_name} to be ready${NC}"
        return 1
    }
    
    # Step 1: Start the development environment
    echo -e "${YELLOW}Step 1: Starting Docker development environment...${NC}"
    echo "Starting services in detached mode..."
    DOCKER_ARGS="-d" make dev-up
    if [ $? -ne 0 ]; then
        echo -e "${RED}Failed to start development environment${NC}"
        exit 1
    fi
    
    # Step 2: Wait for services to be healthy
    echo -e "${YELLOW}Step 2: Checking service health...${NC}"
    
    # Check PostgreSQL
    if ! check_service_health "PostgreSQL" "docker-compose exec -T postgres pg_isready -U pcas"; then
        exit 1
    fi
    
    # Check PCAS (gRPC service)
    if ! check_service_health "PCAS" "docker-compose ps pcas | grep -q 'Up'"; then
        echo "Checking container logs..."
        docker-compose logs pcas | tail -20
        exit 1
    fi
    
    # Give PCAS a bit more time to fully initialize
    echo "Waiting for PCAS to fully initialize..."
    sleep 5
    
    # Test gRPC connectivity before proceeding
    echo "Testing gRPC connectivity..."
    for i in {1..10}; do
        if nc -zv 127.0.0.1 50051 2>&1 | grep -q "succeeded"; then
            echo "gRPC port is open and accepting connections"
            break
        fi
        echo "Waiting for gRPC service... (attempt $i/10)"
        sleep 2
    done
    
    # Wait for PCAS to actually start serving
    echo "Waiting for PCAS server to start..."
    MAX_WAIT=60
    WAITED=0
    while [ $WAITED -lt $MAX_WAIT ]; do
        if docker-compose logs --no-color pcas 2>&1 | grep -q "PCAS server starting on"; then
            echo "PCAS server has started!"
            docker-compose logs --no-color pcas | tail -5
            break
        fi
        echo "Still waiting for PCAS to start... ($WAITED/$MAX_WAIT seconds)"
        sleep 5
        WAITED=$((WAITED + 5))
    done
    
    if [ $WAITED -ge $MAX_WAIT ]; then
        echo "Timeout waiting for PCAS to start. Last logs:"
        docker-compose logs --no-color pcas | tail -30
        exit 1
    fi
    
    # Give it a bit more time to be fully ready
    echo "Giving PCAS additional time to be fully ready..."
    sleep 5
    
    # Step 3: Emit a test event
    echo -e "${YELLOW}Step 3: Emitting test event...${NC}"
    echo "Event text: \"${TEST_EVENT_TEXT}\""
    
    if ! ./bin/pcasctl emit --server "127.0.0.1:50051" --type "pcas.memory.create.v1" --subject "${TEST_EVENT_TEXT}"; then
        echo -e "${RED}Failed to emit test event${NC}"
        exit 1
    fi
    echo -e "${GREEN}Event emitted successfully${NC}"
    
    # Give the system a moment to process and index the event
    echo "Waiting for event processing..."
    sleep 3
    
    # Step 4: Search for the event
    echo -e "${YELLOW}Step 4: Searching for the event...${NC}"
    echo "Search query: \"${SEARCH_QUERY}\""
    
    # Check if we have a real OpenAI API key
    if [[ "${OPENAI_API_KEY}" == "dummy-key-for-testing" ]] || [[ -z "${OPENAI_API_KEY}" ]]; then
        echo -e "${YELLOW}Skipping search test - no valid OpenAI API key available${NC}"
        echo -e "${GREEN}Event emission test PASSED${NC}"
        echo -e "${GREEN}Note: Full validation requires a valid OpenAI API key for search functionality${NC}"
        exit 0
    fi
    
    # Capture the search output
    SEARCH_OUTPUT=$(./bin/pcasctl search --server "127.0.0.1:50051" "${SEARCH_QUERY}" 2>&1) || {
        echo -e "${RED}Search command failed${NC}"
        echo "Output: ${SEARCH_OUTPUT}"
        exit 1
    }
    
    echo "Search output:"
    echo "${SEARCH_OUTPUT}"
    
    # Step 5: Validate the search results
    echo -e "${YELLOW}Step 5: Validating search results...${NC}"
    
    # Check if the search output contains our test event text
    if echo "${SEARCH_OUTPUT}" | grep -q "${TEST_EVENT_TEXT}"; then
        echo -e "${GREEN}✅ Validation PASSED: Found the test event in search results${NC}"
        echo -e "${GREEN}Quick Start flow is working correctly!${NC}"
        exit 0
    else
        echo -e "${RED}❌ Validation FAILED: Test event not found in search results${NC}"
        echo -e "${RED}Expected to find: \"${TEST_EVENT_TEXT}\"${NC}"
        echo -e "${RED}Quick Start flow validation failed${NC}"
        exit 1
    fi
}

# Main execution
case $MODE in
    sqlite)
        run_sqlite_tests
        ;;
    docker)
        run_docker_tests
        ;;
esac