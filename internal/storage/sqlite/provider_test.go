package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func TestSQLiteProvider(t *testing.T) {
	// Create a temporary database file
	dbPath := "test_pcas.db"
	defer os.Remove(dbPath)

	// Create provider
	provider, err := NewProvider(dbPath)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Create test event
	eventData := map[string]interface{}{
		"key": "value",
		"number": 42,
	}
	value, err := structpb.NewValue(eventData)
	if err != nil {
		t.Fatalf("Failed to create structpb value: %v", err)
	}
	anyData, err := anypb.New(value)
	if err != nil {
		t.Fatalf("Failed to create any data: %v", err)
	}

	event := &eventsv1.Event{
		Id:            "test-event-1",
		Type:          "test.event",
		Source:        "test-source",
		Subject:       "test-subject",
		Specversion:   "1.0",
		Time:          timestamppb.New(time.Now()),
		Data:          anyData,
		TraceId:       "trace-123",
		CorrelationId: "correlation-456",
	}

	// Test StoreEvent
	ctx := context.Background()
	err = provider.StoreEvent(ctx, event, nil)
	if err != nil {
		t.Fatalf("Failed to store event: %v", err)
	}

	// Test GetEventByID
	retrievedEvent, err := provider.GetEventByID(ctx, event.Id)
	if err != nil {
		t.Fatalf("Failed to retrieve event: %v", err)
	}

	// Verify event fields
	if retrievedEvent.Id != event.Id {
		t.Errorf("Expected ID %s, got %s", event.Id, retrievedEvent.Id)
	}
	if retrievedEvent.Type != event.Type {
		t.Errorf("Expected Type %s, got %s", event.Type, retrievedEvent.Type)
	}
	if retrievedEvent.Source != event.Source {
		t.Errorf("Expected Source %s, got %s", event.Source, retrievedEvent.Source)
	}
	if retrievedEvent.Subject != event.Subject {
		t.Errorf("Expected Subject %s, got %s", event.Subject, retrievedEvent.Subject)
	}
	if retrievedEvent.TraceId != event.TraceId {
		t.Errorf("Expected TraceId %s, got %s", event.TraceId, retrievedEvent.TraceId)
	}
	if retrievedEvent.CorrelationId != event.CorrelationId {
		t.Errorf("Expected CorrelationId %s, got %s", event.CorrelationId, retrievedEvent.CorrelationId)
	}

	// Test non-existent event
	_, err = provider.GetEventByID(ctx, "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent event, got nil")
	}
}

func TestSQLiteProviderCGO(t *testing.T) {
	// This test will run with CGO version when using build tag
	TestSQLiteProvider(t)
}

func TestSQLiteProviderPure(t *testing.T) {
	// This test will run with pure Go version when not using build tag
	TestSQLiteProvider(t)
}