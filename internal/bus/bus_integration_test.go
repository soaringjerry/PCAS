package bus_test

import (
	"context"
	"testing"
	
	"github.com/soaringjerry/pcas/internal/storage"
	"time"

	"github.com/google/uuid"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"github.com/soaringjerry/pcas/internal/bus"
	"github.com/soaringjerry/pcas/internal/policy"
	"github.com/soaringjerry/pcas/internal/providers"
	"github.com/soaringjerry/pcas/internal/providers/mock"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	events map[string]*eventsv1.Event
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		events: make(map[string]*eventsv1.Event),
	}
}

func (m *mockStorage) StoreEvent(ctx context.Context, event *eventsv1.Event, embedding []float32) error {
	m.events[event.Id] = event
	return nil
}

func (m *mockStorage) GetEventByID(ctx context.Context, eventID string) (*eventsv1.Event, error) {
	event, ok := m.events[eventID]
	if !ok {
		return nil, nil
	}
	return event, nil
}

func (m *mockStorage) BatchGetEvents(ctx context.Context, ids []string) ([]*eventsv1.Event, error) {
	events := make([]*eventsv1.Event, 0, len(ids))
	for _, id := range ids {
		if event, ok := m.events[id]; ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func (m *mockStorage) GetAllEvents(ctx context.Context, limit int, offset int) ([]*eventsv1.Event, error) {
	events := make([]*eventsv1.Event, 0)
	for _, event := range m.events {
		events = append(events, event)
	}
	// Simple pagination
	if offset >= len(events) {
		return []*eventsv1.Event{}, nil
	}
	end := offset + limit
	if end > len(events) {
		end = len(events)
	}
	return events[offset:end], nil
}

func (m *mockStorage) QuerySimilar(ctx context.Context, embedding []float32, topK int, filter *storage.Filter) ([]storage.QueryResult, error) {
	// Simple mock implementation - return empty results
	return []storage.QueryResult{}, nil
}

func (m *mockStorage) AddEmbeddingToEvent(ctx context.Context, eventID string, embedding []float32) error {
	// Mock implementation - just return success
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

func TestEchoEventRouting(t *testing.T) {
	// 1. Load policy configuration
	policyConfig, err := policy.LoadPolicy("../../policy.yaml")
	if err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}
	policyEngine := policy.NewEngine(policyConfig)

	// 2. Create MockProvider
	mockProvider := mock.NewProvider()

	// 3. Create provider map
	providerMap := map[string]providers.ComputeProvider{
		"mock-provider": mockProvider,
	}

	// 4. Create mock storage
	storage := newMockStorage()

	// 5. Create bus server
	server := bus.NewServer(policyEngine, providerMap, storage)

	// 6. Create a test context
	ctx := context.Background()
	
	// Note: Since we can't easily access the server's internal subscriber management
	// without gRPC, we'll verify the integration by checking the storage for the
	// response event after publishing
	
	// 7. Create pcas.echo.v1 event
	echoEvent := &eventsv1.Event{
		Id:          uuid.New().String(),
		Type:        "pcas.echo.v1",
		Source:      "integration-test",
		Specversion: "1.0",
		Time:        timestamppb.New(time.Now()),
		Subject:     "test-echo",
	}

	// Add some test data to the event
	testData := map[string]interface{}{
		"message": "Hello from integration test",
		"test_id": "123",
	}
	structData, err := structpb.NewValue(testData)
	if err != nil {
		t.Fatalf("Failed to create event data: %v", err)
	}
	echoEvent.Data, _ = anypb.New(structData)

	// 8. Publish the echo event
	_, err = server.Publish(ctx, echoEvent)
	if err != nil {
		t.Fatalf("Failed to publish echo event: %v", err)
	}

	// 9. Wait a short time for processing
	time.Sleep(100 * time.Millisecond)

	// 10. Check that the response event was created and stored
	// The response event should have type "pcas.response.v1"
	// and should contain the MockProvider's response
	var responseEvent *eventsv1.Event
	for _, event := range storage.events {
		if event.Type == "pcas.response.v1" && event.CorrelationId == echoEvent.Id {
			responseEvent = event
			break
		}
	}

	if responseEvent == nil {
		t.Fatal("No response event found")
	}

	// 11. Verify the response contains MockProvider's output
	if responseEvent.Data == nil {
		t.Fatal("Response event has no data")
	}

	// Extract and verify the response data
	value := &structpb.Value{}
	if err := responseEvent.Data.UnmarshalTo(value); err != nil {
		t.Fatalf("Failed to unmarshal response data: %v", err)
	}

	responseData, ok := value.AsInterface().(map[string]interface{})
	if !ok {
		t.Fatal("Response data is not a map")
	}

	// Check that the response contains expected fields
	if responseData["original_event_id"] != echoEvent.Id {
		t.Errorf("Expected original_event_id to be %s, got %v", echoEvent.Id, responseData["original_event_id"])
	}

	if responseData["provider"] != "mock-provider" {
		t.Errorf("Expected provider to be mock-provider, got %v", responseData["provider"])
	}

	// The key assertion: MockProvider should return its fixed response
	expectedResponse := "Mock response from mock-provider"
	if responseData["response"] != expectedResponse {
		t.Errorf("Expected response to be %q, got %v", expectedResponse, responseData["response"])
	}

	t.Logf("✅ Echo event successfully routed to MockProvider and response received: %s", responseData["response"])
}