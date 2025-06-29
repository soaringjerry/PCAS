package sqlite

import (
	"context"
	"fmt"
	"testing"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorStorage(t *testing.T) {
	// Create a temporary database
	provider, err := NewProvider(":memory:")
	require.NoError(t, err)
	defer provider.Close()
	
	ctx := context.Background()
	
	// Test vector
	testVector := make([]float32, 768)
	for i := range testVector {
		testVector[i] = float32(i) / 768.0
	}
	
	// Create test event
	event := &eventsv1.Event{
		Id:          "test-event-1",
		Type:        "test.event.type",
		Source:      "test-source",
		Specversion: "1.0",
	}
	
	// Store event with embedding
	err = provider.StoreEvent(ctx, event, testVector)
	require.NoError(t, err)
	
	// Query similar events
	results, err := provider.QuerySimilar(ctx, testVector, 5, nil)
	require.NoError(t, err)
	
	// Verify results
	assert.Len(t, results, 1)
	assert.Equal(t, "test-event-1", results[0].ID)
	assert.InDelta(t, 1.0, results[0].Score, 0.01) // Should be very similar to itself
}

func TestMultipleVectorStorage(t *testing.T) {
	// Create a temporary database
	provider, err := NewProvider(":memory:")
	require.NoError(t, err)
	defer provider.Close()
	
	ctx := context.Background()
	
	// Store multiple events with different embeddings
	for i := 0; i < 5; i++ {
		// Create slightly different vectors
		vec := make([]float32, 768)
		for j := range vec {
			vec[j] = float32(j+i) / 768.0
		}
		
		event := &eventsv1.Event{
			Id:          fmt.Sprintf("event-%d", i),
			Type:        "test.event.type",
			Source:      "test-source",
			Specversion: "1.0",
		}
		
		err = provider.StoreEvent(ctx, event, vec)
		require.NoError(t, err)
	}
	
	// Query with a vector similar to the first one
	queryVec := make([]float32, 768)
	for i := range queryVec {
		queryVec[i] = float32(i) / 768.0
	}
	
	results, err := provider.QuerySimilar(ctx, queryVec, 3, nil)
	require.NoError(t, err)
	
	// Verify we got top 3 results
	assert.Len(t, results, 3)
	
	// First result should be event-0 (most similar)
	assert.Equal(t, "event-0", results[0].ID)
	
	// Scores should be in descending order
	for i := 1; i < len(results); i++ {
		assert.LessOrEqual(t, results[i].Score, results[i-1].Score)
	}
}