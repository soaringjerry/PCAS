# End-to-End Testing

This document describes the end-to-end (E2E) testing strategy for PCAS.

## Quick Start Validation

The primary E2E test validates the Quick Start flow from our README.md.

### What It Tests

1. **Environment Setup**: Ensures Docker Compose can start ChromaDB and PCAS
2. **Service Health**: Verifies both services are healthy and responsive
3. **Event Emission**: Tests that events can be stored via `pcasctl emit`
4. **Semantic Search**: Validates that stored events can be found via `pcasctl search`
5. **Integration**: Confirms all components work together correctly

### Running Locally

To run the validation script locally:

```bash
# Option 1: Direct execution (requires manual cleanup)
./scripts/validate_quickstart.sh

# Option 2: With test wrapper (recommended)
./scripts/test_quickstart_local.sh
```

### CI Integration

The validation runs automatically in CI on:
- Every push to `main` or `dev` branches
- Every pull request

The CI job:
1. Builds the binaries
2. Sets up Docker Compose
3. Runs the validation script
4. Reports success/failure

### Script Details

**Location**: `scripts/validate_quickstart.sh`

**Key Features**:
- Automatic cleanup on exit (success or failure)
- Colored output for better readability
- Configurable timeouts and retry intervals
- Clear error messages for debugging

**Configuration**:
- `MAX_WAIT_TIME`: Maximum seconds to wait for services (default: 60)
- `WAIT_INTERVAL`: Seconds between health checks (default: 2)
- `TEST_EVENT_TEXT`: The test event content
- `SEARCH_QUERY`: The semantic search query

### Troubleshooting

If the validation fails:

1. **Check Service Logs**:
   ```bash
   make dev-logs
   ```

2. **Verify Services Manually**:
   ```bash
   # Start services
   make dev-up
   
   # In another terminal, check health
   curl http://localhost:8000/api/v1/heartbeat
   ./bin/pcasctl ping
   ```

3. **Run Components Individually**:
   ```bash
   # Start environment
   make dev-up -d
   
   # Emit event
   ./bin/pcasctl emit "Test event"
   
   # Search
   ./bin/pcasctl search "test"
   
   # Cleanup
   make dev-down
   ```

### Future Enhancements

- Add more E2E scenarios (multiple events, complex queries)
- Performance benchmarking
- Load testing
- Multi-language event testing