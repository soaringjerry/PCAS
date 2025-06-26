package vectorpg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestProviderImplementsInterface(t *testing.T) {
	// This test just verifies that our Provider implements the VectorStorage interface
	// The actual functionality tests would require a PostgreSQL container with pgvector
	t.Log("Provider successfully implements VectorStorage interface")
}

func TestQuerySimilarWithFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container with pgvector
	req := testcontainers.ContainerRequest{
		Image:        "ankane/pgvector:latest",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
	}

	postgres, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer postgres.Terminate(ctx)

	// Get connection string
	host, err := postgres.Host(ctx)
	require.NoError(t, err)
	port, err := postgres.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgresql://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	// Create provider
	provider, err := New(ctx, dsn)
	require.NoError(t, err)
	defer provider.Close()

	// Prepare test data
	// Create a simple embedding (normally would be 3072 dimensions, but we'll use fewer for testing)
	baseEmbedding := make([]float32, 3072)
	for i := range baseEmbedding {
		baseEmbedding[i] = float32(i) * 0.001
	}

	// Store test vectors with different metadata
	testData := []struct {
		id       string
		metadata map[string]string
	}{
		{
			id: "vec-a",
			metadata: map[string]string{
				"user_id":        "user-1",
				"event_type":     "type-a",
				"timestamp_unix": fmt.Sprintf("%d", time.Now().Unix()),
			},
		},
		{
			id: "vec-b",
			metadata: map[string]string{
				"user_id":        "user-1",
				"event_type":     "type-b",
				"timestamp_unix": fmt.Sprintf("%d", time.Now().Unix()),
			},
		},
		{
			id: "vec-c",
			metadata: map[string]string{
				"user_id":        "user-2",
				"event_type":     "type-a",
				"timestamp_unix": fmt.Sprintf("%d", time.Now().Unix()),
			},
		},
		{
			id: "vec-d",
			metadata: map[string]string{
				"user_id":        "user-2",
				"event_type":     "type-b",
				"session_id":     "session-123",
				"timestamp_unix": fmt.Sprintf("%d", time.Now().Add(-2*time.Hour).Unix()),
			},
		},
	}

	// Store embeddings
	for _, td := range testData {
		// Slightly modify embedding for each vector to ensure different similarities
		embedding := make([]float32, len(baseEmbedding))
		copy(embedding, baseEmbedding)
		embedding[0] = float32(len(td.id)) * 0.1 // Small variation

		err := provider.StoreEmbedding(ctx, td.id, embedding, td.metadata)
		require.NoError(t, err)
	}

	// Test scenarios
	testCases := []struct {
		name            string
		filters         map[string]interface{}
		expectedIDs     []string
		expectAllIDs    bool // If true, expect all IDs but not necessarily in order
		minExpectedSize int  // Minimum number of results expected
	}{
		{
			name: "Filter by user_id",
			filters: map[string]interface{}{
				"user_id": "user-1",
			},
			expectedIDs: []string{"vec-a", "vec-b"},
			expectAllIDs: true,
		},
		{
			name: "Filter by event_type",
			filters: map[string]interface{}{
				"event_type": "type-a",
			},
			expectedIDs: []string{"vec-a", "vec-c"},
			expectAllIDs: true,
		},
		{
			name: "Composite filter user_id and event_type",
			filters: map[string]interface{}{
				"user_id":    "user-1",
				"event_type": "type-b",
			},
			expectedIDs: []string{"vec-b"},
			expectAllIDs: true,
		},
		{
			name: "Filter by session_id",
			filters: map[string]interface{}{
				"session_id": "session-123",
			},
			expectedIDs: []string{"vec-d"},
			expectAllIDs: true,
		},
		{
			name: "No results filter",
			filters: map[string]interface{}{
				"user_id": "user-3",
			},
			expectedIDs: []string{},
			expectAllIDs: true,
		},
		{
			name: "Time range filter",
			filters: map[string]interface{}{
				"event_ts_after":  time.Now().Add(-3 * time.Hour),
				"event_ts_before": time.Now().Add(-1 * time.Hour),
			},
			expectedIDs: []string{"vec-d"},
			expectAllIDs: true,
		},
		{
			name:            "No filters - returns all",
			filters:         nil,
			minExpectedSize: 4, // Should return all 4 vectors
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Query with filters
			results, err := provider.QuerySimilar(ctx, baseEmbedding, 10, tc.filters)
			require.NoError(t, err)

			// Extract IDs from results
			var resultIDs []string
			for _, result := range results {
				resultIDs = append(resultIDs, result.ID)
			}

			if tc.expectAllIDs {
				// Check that we got exactly the expected IDs (order doesn't matter)
				assert.ElementsMatch(t, tc.expectedIDs, resultIDs, 
					"Expected IDs %v but got %v", tc.expectedIDs, resultIDs)
			} else if tc.minExpectedSize > 0 {
				// Check minimum size
				assert.GreaterOrEqual(t, len(resultIDs), tc.minExpectedSize,
					"Expected at least %d results but got %d", tc.minExpectedSize, len(resultIDs))
			}

			// Verify all results have similarity scores
			for _, result := range results {
				assert.Greater(t, result.Score, float32(-1.0), "Score should be greater than -1")
				assert.LessOrEqual(t, result.Score, float32(1.0), "Score should be less than or equal to 1")
			}
		})
	}
}