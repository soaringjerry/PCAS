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

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up environment...${NC}"
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

# Main script
echo -e "${GREEN}=== PCAS Quick Start Validation Script ===${NC}"
echo "This script validates the Quick Start flow from README.md"
echo

# Step 1: Start the development environment
echo -e "${YELLOW}Step 1: Starting development environment...${NC}"
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
# Check if the container is running
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

# Check PCAS logs to ensure it's fully started
echo "Checking PCAS service logs..."
docker-compose logs --no-color pcas | tail -20

# Check if PCAS is actually listening on the port inside the container
echo "Checking if PCAS is listening inside container..."
docker-compose exec -T pcas netstat -tlnp 2>/dev/null | grep 50051 || echo "netstat not available"

# Additional wait for service initialization
echo "Giving PCAS more time to initialize..."
sleep 10

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