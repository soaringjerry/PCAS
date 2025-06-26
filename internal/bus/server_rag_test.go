package bus

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/storage"
)

// Mock types for testing
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) StoreEvent(ctx context.Context, event *eventsv1.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockStorage) GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error) {
	args := m.Called(ctx, eventID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*eventsv1.Event), args.Error(1)
}

func (m *MockStorage) BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*eventsv1.Event), args.Error(1)
}

func (m *MockStorage) Close() error {
	args := m.Called()
	return args.Error(0)
}

type MockVectorStorage struct {
	mock.Mock
}

func (m *MockVectorStorage) StoreEmbedding(ctx context.Context, eventID string, embedding []float32, metadata map[string]string) error {
	args := m.Called(ctx, eventID, embedding, metadata)
	return args.Error(0)
}

func (m *MockVectorStorage) QuerySimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]storage.QueryResult, error) {
	args := m.Called(ctx, queryEmbedding, topK)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.QueryResult), args.Error(1)
}

func (m *MockVectorStorage) Close() error {
	args := m.Called()
	return args.Error(0)
}

type MockEmbeddingProvider struct {
	mock.Mock
}

func (m *MockEmbeddingProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]float32), args.Error(1)
}

func (m *MockEmbeddingProvider) Name() string {
	return "mock-embedding-provider"
}

// Test embedding cache
func TestEmbeddingCache(t *testing.T) {
	cache := newEmbeddingCache(3)
	
	// Test Set and Get
	embedding1 := []float32{1.0, 2.0, 3.0}
	cache.Set("key1", embedding1)
	
	result, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, embedding1, result)
	
	// Test miss
	_, found = cache.Get("nonexistent")
	assert.False(t, found)
	
	// Test LRU eviction
	cache.Set("key2", []float32{2.0, 3.0, 4.0})
	cache.Set("key3", []float32{3.0, 4.0, 5.0})
	cache.Set("key4", []float32{4.0, 5.0, 6.0}) // Should evict key1
	
	_, found = cache.Get("key1")
	assert.False(t, found) // key1 should be evicted
	
	// Verify other keys still exist
	_, found = cache.Get("key2")
	assert.True(t, found)
	_, found = cache.Get("key3")
	assert.True(t, found)
	_, found = cache.Get("key4")
	assert.True(t, found)
	
	// Test metrics
	assert.Equal(t, int64(4), cache.hits)
	assert.Equal(t, int64(2), cache.misses)
}

// Test generateQueryText
func TestGenerateQueryText(t *testing.T) {
	s := &Server{}
	
	tests := []struct {
		name        string
		event       *eventsv1.Event
		requestData map[string]interface{}
		expected    string
	}{
		{
			name: "event type and subject",
			event: &eventsv1.Event{
				Type:    "compute.request",
				Subject: "calculate prime numbers",
			},
			requestData: nil,
			expected:    "calculate prime numbers",
		},
		{
			name: "with query in request data",
			event: &eventsv1.Event{
				Type: "search.request",
			},
			requestData: map[string]interface{}{
				"query": "find similar documents",
			},
			expected: "find similar documents",
		},
		{
			name: "with prompt in request data",
			event: &eventsv1.Event{
				Type: "llm.request",
			},
			requestData: map[string]interface{}{
				"prompt": "explain quantum computing",
			},
			expected: "explain quantum computing",
		},
		{
			name: "multiple fields",
			event: &eventsv1.Event{
				Type:    "chat.message",
				Subject: "user question",
			},
			requestData: map[string]interface{}{
				"message": "how does RAG work?",
				"text":    "additional context",
			},
			expected: "user question how does RAG work? additional context",
		},
		{
			name:        "empty event",
			event:       &eventsv1.Event{},
			requestData: nil,
			expected:    "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.generateQueryText(tt.event, tt.requestData)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test renderSingleEventMarkdown
func TestRenderSingleEventMarkdown(t *testing.T) {
	s := &Server{}
	
	// Create test event with data
	testData := map[string]interface{}{
		"prompt": "test prompt",
		"response": "test response",
		"key1": "value1",
		"key2": 42,
	}
	structData, _ := structpb.NewValue(testData)
	anyData, _ := anypb.New(structData)
	
	event := &eventsv1.Event{
		Id:          "test-123",
		Type:        "test.event",
		Source:      "test-source",
		Subject:     "test subject",
		Time:        timestamppb.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
		Data:        anyData,
	}
	
	markdown := s.renderSingleEventMarkdown(event)
	
	// Check key components are present in new concise format
	assert.Contains(t, markdown, "**[2024-01-01 12:00]**")
	assert.Contains(t, markdown, "test.event: test subject")
	assert.Contains(t, markdown, "- prompt: test prompt")
	assert.Contains(t, markdown, "- response: test response")
}

// Test renderEventsMarkdown with token limit
func TestRenderEventsMarkdown(t *testing.T) {
	s := &Server{}
	
	// Create multiple test events
	events := make([]*eventsv1.Event, 10)
	for i := 0; i < 10; i++ {
		events[i] = &eventsv1.Event{
			Id:     fmt.Sprintf("event-%d", i),
			Type:   "test.event",
			Source: "test-source",
		}
	}
	
	// Test with small token limit
	markdown := s.renderEventsMarkdown(events, 200)
	
	// Should contain header
	assert.Contains(t, markdown, "## Relevant Historical Context")
	
	// Should contain truncation message (since each event is short, we need more events)
	// Create many more events to trigger truncation
	manyEvents := make([]*eventsv1.Event, 100)
	for i := 0; i < 100; i++ {
		manyEvents[i] = &eventsv1.Event{
			Id:     fmt.Sprintf("event-%d", i),
			Type:   "test.event",
			Source: "test-source",
			Subject: fmt.Sprintf("This is a long test subject for event %d that should consume more tokens", i),
		}
	}
	markdownTruncated := s.renderEventsMarkdown(manyEvents, 200)
	assert.Contains(t, markdownTruncated, "more relevant events (truncated due to token limit)")
	
	// Test with large token limit
	markdown = s.renderEventsMarkdown(events, 50000)
	
	// Should contain event types (not IDs in the new format)
	assert.Contains(t, markdown, "test.event")
}

// Test applyRAGEnhancement with mocks
func TestApplyRAGEnhancement(t *testing.T) {
	// Set environment variable
	os.Setenv("PCAS_RAG_ENABLED", "true")
	defer os.Unsetenv("PCAS_RAG_ENABLED")
	
	// Create mocks
	mockStorage := new(MockStorage)
	mockVectorStorage := new(MockVectorStorage)
	mockEmbeddingProvider := new(MockEmbeddingProvider)
	
	// Create server
	s := &Server{
		storage:           mockStorage,
		vectorStorage:     mockVectorStorage,
		embeddingProvider: mockEmbeddingProvider,
		embeddingCache:    newEmbeddingCache(100),
		rateLimiter:       rate.NewLimiter(rate.Every(time.Second), 10),
		singleFlight:      &singleflight.Group{},
	}
	
	// Test event
	event := &eventsv1.Event{
		Id:      "current-event",
		Type:    "compute.request",
		Subject: "test computation",
	}
	
	requestData := make(map[string]interface{})
	
	// Mock embedding creation
	testEmbedding := []float32{0.1, 0.2, 0.3}
	mockEmbeddingProvider.On("CreateEmbedding", mock.Anything, "test computation").
		Return(testEmbedding, nil)
	
	// Mock vector search
	similarResults := []storage.QueryResult{
		{ID: "event-1", Score: 0.9},
		{ID: "event-2", Score: 0.8},
		{ID: "current-event", Score: 1.0}, // Should be filtered out
		{ID: "event-3", Score: 0.3}, // Below threshold (0.4)
	}
	mockVectorStorage.On("QuerySimilar", mock.Anything, testEmbedding, ragTopK).
		Return(similarResults, nil)
	
	// Mock batch get events
	relevantEvents := []*eventsv1.Event{
		{
			Id:     "event-1",
			Type:   "compute.response",
			Source: "compute-provider",
		},
		{
			Id:     "event-2",
			Type:   "compute.request",
			Source: "client",
		},
	}
	mockStorage.On("BatchGetEvents", mock.Anything, []string{"event-1", "event-2"}).
		Return(relevantEvents, nil)
	
	// Execute
	ctx := context.Background()
	s.applyRAGEnhancement(ctx, event, requestData)
	
	// Verify results
	ragApplied, exists := requestData["rag_applied"]
	assert.True(t, exists, "rag_applied should be set")
	if exists {
		assert.True(t, ragApplied.(bool))
	}
	
	ragEventCount, exists := requestData["rag_event_count"]
	assert.True(t, exists, "rag_event_count should be set") 
	if exists {
		assert.Equal(t, 2, ragEventCount)
	}
	// In the new implementation, we don't set rag_context, but messages
	messages, exists := requestData["messages"]
	assert.True(t, exists, "messages should be set")
	if exists {
		assert.NotEmpty(t, messages)
	}
	
	// Verify mocks were called
	mockEmbeddingProvider.AssertExpectations(t)
	mockVectorStorage.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

// Test applyRAGEnhancement when disabled
func TestApplyRAGEnhancementDisabled(t *testing.T) {
	// Ensure RAG is disabled
	os.Unsetenv("PCAS_RAG_ENABLED")
	
	// Create mocks (should not be called)
	mockStorage := new(MockStorage)
	mockVectorStorage := new(MockVectorStorage)
	mockEmbeddingProvider := new(MockEmbeddingProvider)
	
	// Create server
	s := &Server{
		storage:           mockStorage,
		vectorStorage:     mockVectorStorage,
		embeddingProvider: mockEmbeddingProvider,
		embeddingCache:    newEmbeddingCache(100),
		rateLimiter:       rate.NewLimiter(rate.Every(time.Second), 10),
		singleFlight:      &singleflight.Group{},
	}
	
	event := &eventsv1.Event{Id: "test"}
	requestData := make(map[string]interface{})
	
	// Execute
	ctx := context.Background()
	s.applyRAGEnhancement(ctx, event, requestData)
	
	// Verify no RAG was applied
	_, hasRagApplied := requestData["rag_applied"]
	assert.False(t, hasRagApplied)
	
	// Verify no mocks were called
	mockEmbeddingProvider.AssertNotCalled(t, "CreateEmbedding")
	mockVectorStorage.AssertNotCalled(t, "QuerySimilar")
	mockStorage.AssertNotCalled(t, "BatchGetEvents")
}

// Test applyRAGEnhancement with timeout
func TestApplyRAGEnhancementTimeout(t *testing.T) {
	os.Setenv("PCAS_RAG_ENABLED", "true")
	defer os.Unsetenv("PCAS_RAG_ENABLED")
	
	// Create mocks
	mockStorage := new(MockStorage)
	mockVectorStorage := new(MockVectorStorage)
	mockEmbeddingProvider := new(MockEmbeddingProvider)
	
	// Create server
	s := &Server{
		storage:           mockStorage,
		vectorStorage:     mockVectorStorage,
		embeddingProvider: mockEmbeddingProvider,
		embeddingCache:    newEmbeddingCache(100),
		rateLimiter:       rate.NewLimiter(rate.Every(time.Second), 10),
		singleFlight:      &singleflight.Group{},
	}
	
	event := &eventsv1.Event{
		Id:   "test",
		Type: "test.event",
	}
	requestData := make(map[string]interface{})
	
	// Mock embedding creation with delay
	mockEmbeddingProvider.On("CreateEmbedding", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			// Simulate slow embedding generation
			time.Sleep(3 * time.Second)
		}).
		Return([]float32{}, context.DeadlineExceeded)
	
	// Execute with a context that will timeout
	ctx := context.Background()
	s.applyRAGEnhancement(ctx, event, requestData)
	
	// Verify no RAG was applied due to timeout
	_, hasRagApplied := requestData["rag_applied"]
	assert.False(t, hasRagApplied)
}

// Test cache hit scenario
func TestApplyRAGEnhancementCacheHit(t *testing.T) {
	os.Setenv("PCAS_RAG_ENABLED", "true")
	defer os.Unsetenv("PCAS_RAG_ENABLED")
	
	// Create mocks
	mockStorage := new(MockStorage)
	mockVectorStorage := new(MockVectorStorage)
	mockEmbeddingProvider := new(MockEmbeddingProvider)
	
	// Create server
	s := &Server{
		storage:           mockStorage,
		vectorStorage:     mockVectorStorage,
		embeddingProvider: mockEmbeddingProvider,
		embeddingCache:    newEmbeddingCache(100),
		rateLimiter:       rate.NewLimiter(rate.Every(time.Second), 10),
		singleFlight:      &singleflight.Group{},
	}
	
	event := &eventsv1.Event{
		Id:   "test",
		Type: "test.event",
		Subject: "test query",
	}
	requestData := make(map[string]interface{})
	
	// Pre-populate cache
	testEmbedding := []float32{0.1, 0.2, 0.3}
	cacheKey := "rag:test query"
	s.embeddingCache.Set(cacheKey, testEmbedding)
	
	// Mock vector search (embedding provider should NOT be called due to cache hit)
	mockVectorStorage.On("QuerySimilar", mock.Anything, testEmbedding, ragTopK).
		Return([]storage.QueryResult{}, nil)
	
	// Execute
	ctx := context.Background()
	s.applyRAGEnhancement(ctx, event, requestData)
	
	// Verify embedding provider was NOT called (cache hit)
	mockEmbeddingProvider.AssertNotCalled(t, "CreateEmbedding")
	mockVectorStorage.AssertExpectations(t)
}