package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockSubscribeStream implements the SubscribeServer interface for testing
type mockSubscribeStream struct {
	grpc.ServerStream
	ctx      context.Context
	cancel   context.CancelFunc
	events   chan *eventsv1.Event
	sendErr  error
}

func newMockSubscribeStream() *mockSubscribeStream {
	ctx, cancel := context.WithCancel(context.Background())
	return &mockSubscribeStream{
		ctx:    ctx,
		cancel: cancel,
		events: make(chan *eventsv1.Event, 100),
	}
}

func (m *mockSubscribeStream) Send(event *eventsv1.Event) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	select {
	case m.events <- event:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *mockSubscribeStream) Context() context.Context {
	return m.ctx
}

func TestMemoryBus_PublishSubscribe(t *testing.T) {
	bus := NewMemoryBus()

	// Create a test event
	testEvent := &eventsv1.Event{
		Id:     "test-event-1",
		Type:   "test.event.v1",
		Source: "test-source",
		Time:   timestamppb.Now(),
	}

	// Test 1: Subscribe a client
	stream1 := newMockSubscribeStream()
	go func() {
		err := bus.Subscribe(&busv1.SubscribeRequest{
			ClientId: "client-1",
		}, stream1)
		if err != nil {
			t.Logf("Subscribe ended with error: %v", err)
		}
	}()

	// Give subscription time to establish
	time.Sleep(50 * time.Millisecond)

	// Verify subscriber count
	if count := bus.GetSubscriberCount(); count != 1 {
		t.Errorf("expected 1 subscriber, got %d", count)
	}

	// Test 2: Publish an event
	_, err := bus.Publish(context.Background(), testEvent)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Verify the subscriber received the event
	select {
	case received := <-stream1.events:
		if received.Id != testEvent.Id {
			t.Errorf("received event ID %s, expected %s", received.Id, testEvent.Id)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("subscriber did not receive event in time")
	}

	// Test 3: Disconnect subscriber
	stream1.cancel()
	time.Sleep(50 * time.Millisecond)

	// Verify subscriber was removed
	if count := bus.GetSubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after disconnect, got %d", count)
	}
}

func TestMemoryBus_MultipleSubscribers(t *testing.T) {
	bus := NewMemoryBus()

	// Create multiple subscribers
	const numSubscribers = 5
	streams := make([]*mockSubscribeStream, numSubscribers)
	
	for i := 0; i < numSubscribers; i++ {
		streams[i] = newMockSubscribeStream()
		clientID := fmt.Sprintf("client-%d", i)
		
		go func(id string, stream *mockSubscribeStream) {
			err := bus.Subscribe(&busv1.SubscribeRequest{
				ClientId: id,
			}, stream)
			if err != nil {
				t.Logf("Subscribe for %s ended with error: %v", id, err)
			}
		}(clientID, streams[i])
	}

	// Give subscriptions time to establish
	time.Sleep(100 * time.Millisecond)

	// Verify all subscribers are registered
	if count := bus.GetSubscriberCount(); count != numSubscribers {
		t.Errorf("expected %d subscribers, got %d", numSubscribers, count)
	}

	// Publish an event
	testEvent := &eventsv1.Event{
		Id:     "broadcast-event",
		Type:   "test.broadcast.v1",
		Source: "test-source",
		Time:   timestamppb.Now(),
	}

	_, err := bus.Publish(context.Background(), testEvent)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Verify all subscribers received the event
	for i, stream := range streams {
		select {
		case received := <-stream.events:
			if received.Id != testEvent.Id {
				t.Errorf("subscriber %d received wrong event ID: %s", i, received.Id)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d did not receive event in time", i)
		}
	}

	// Clean up
	for _, stream := range streams {
		stream.cancel()
	}
}

func TestMemoryBus_ConcurrentOperations(t *testing.T) {
	bus := NewMemoryBus()
	
	// Number of concurrent operations
	const (
		numPublishers  = 10
		numSubscribers = 10
		numEvents      = 100
	)

	var wg sync.WaitGroup

	// Start subscribers
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			stream := newMockSubscribeStream()
			clientID := fmt.Sprintf("concurrent-client-%d", id)
			
			go func() {
				err := bus.Subscribe(&busv1.SubscribeRequest{
					ClientId: clientID,
				}, stream)
				if err != nil {
					t.Logf("Subscribe for %s ended with error: %v", clientID, err)
				}
			}()

			// Count received events
			eventCount := 0
			timeout := time.After(2 * time.Second)
			
			for {
				select {
				case <-stream.events:
					eventCount++
				case <-timeout:
					t.Logf("Subscriber %d received %d events", id, eventCount)
					stream.cancel()
					return
				}
			}
		}(i)
	}

	// Give subscribers time to establish
	time.Sleep(100 * time.Millisecond)

	// Start publishers
	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(publisherID int) {
			defer wg.Done()
			
			for j := 0; j < numEvents; j++ {
				event := &eventsv1.Event{
					Id:     fmt.Sprintf("event-%d-%d", publisherID, j),
					Type:   "test.concurrent.v1",
					Source: fmt.Sprintf("publisher-%d", publisherID),
					Time:   timestamppb.Now(),
				}
				
				_, err := bus.Publish(context.Background(), event)
				if err != nil {
					t.Errorf("Publisher %d failed to publish event %d: %v", publisherID, j, err)
				}
				
				// Small delay to simulate realistic publishing
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Wait for all operations to complete
	wg.Wait()

	// Verify no subscribers remain
	time.Sleep(100 * time.Millisecond)
	if count := bus.GetSubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after test, got %d", count)
	}
}

func TestMemoryBus_SlowSubscriber(t *testing.T) {
	bus := NewMemoryBus()

	// Create a slow subscriber (we won't read from its channel)
	slowStream := newMockSubscribeStream()
	go func() {
		_ = bus.Subscribe(&busv1.SubscribeRequest{
			ClientId: "slow-client",
		}, slowStream)
	}()

	// Create a normal subscriber
	normalStream := newMockSubscribeStream()
	go func() {
		_ = bus.Subscribe(&busv1.SubscribeRequest{
			ClientId: "normal-client",
		}, normalStream)
	}()

	// Give subscriptions time to establish
	time.Sleep(50 * time.Millisecond)

	// Publish many events to fill the slow subscriber's buffer
	for i := 0; i < 200; i++ {
		event := &eventsv1.Event{
			Id:     fmt.Sprintf("flood-event-%d", i),
			Type:   "test.flood.v1",
			Source: "test-source",
			Time:   timestamppb.Now(),
		}
		
		_, err := bus.Publish(context.Background(), event)
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
	}

	// Verify the normal subscriber can still receive events
	receivedCount := 0
	timeout := time.After(500 * time.Millisecond)
	
drainLoop:
	for {
		select {
		case <-normalStream.events:
			receivedCount++
		case <-timeout:
			break drainLoop
		}
	}

	// The normal subscriber should have received at least some events
	if receivedCount == 0 {
		t.Error("normal subscriber received no events")
	}
	
	t.Logf("Normal subscriber received %d events despite slow subscriber", receivedCount)

	// Clean up
	slowStream.cancel()
	normalStream.cancel()
}

func TestMemoryBus_InvalidRequests(t *testing.T) {
	bus := NewMemoryBus()

	// Test 1: Publish with nil event
	_, err := bus.Publish(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil event, got none")
	}

	// Test 2: Subscribe with empty client ID
	stream := newMockSubscribeStream()
	err = bus.Subscribe(&busv1.SubscribeRequest{
		ClientId: "",
	}, stream)
	if err == nil {
		t.Error("expected error for empty client ID, got none")
	}

	// Test 3: Duplicate subscription
	stream1 := newMockSubscribeStream()
	go func() {
		_ = bus.Subscribe(&busv1.SubscribeRequest{
			ClientId: "duplicate-client",
		}, stream1)
	}()
	
	time.Sleep(50 * time.Millisecond)
	
	stream2 := newMockSubscribeStream()
	err = bus.Subscribe(&busv1.SubscribeRequest{
		ClientId: "duplicate-client",
	}, stream2)
	if err == nil {
		t.Error("expected error for duplicate subscription, got none")
	}
	
	// Clean up
	stream1.cancel()
}