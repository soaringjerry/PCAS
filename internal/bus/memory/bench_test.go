package memory

import (
	"context"
	"testing"
	"time"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkMemoryBus_Publish(b *testing.B) {
	bus := NewMemoryBus()

	// Create a test event
	event := &eventsv1.Event{
		Id:     "bench-event",
		Type:   "bench.test.v1",
		Source: "benchmark",
		Time:   timestamppb.Now(),
	}

	// Add some subscribers
	for i := 0; i < 10; i++ {
		stream := newMockSubscribeStream()
		go func(clientID string) {
			_ = bus.Subscribe(&busv1.SubscribeRequest{
				ClientId: clientID,
			}, stream)
		}(string(rune('a' + i)))
		
		// Drain events in background
		go func(s *mockSubscribeStream) {
			for range s.events {
				// Just drain
			}
		}(stream)
	}

	// Wait for subscribers to be ready
	time.Sleep(10 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := bus.Publish(context.Background(), event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryBus_ConcurrentPublish(b *testing.B) {
	bus := NewMemoryBus()

	// Create a test event
	event := &eventsv1.Event{
		Id:     "bench-event",
		Type:   "bench.test.v1",
		Source: "benchmark",
		Time:   timestamppb.Now(),
	}

	// Add some subscribers
	for i := 0; i < 10; i++ {
		stream := newMockSubscribeStream()
		go func(clientID string) {
			_ = bus.Subscribe(&busv1.SubscribeRequest{
				ClientId: clientID,
			}, stream)
		}(string(rune('a' + i)))
		
		// Drain events in background
		go func(s *mockSubscribeStream) {
			for range s.events {
				// Just drain
			}
		}(stream)
	}

	// Wait for subscribers to be ready
	time.Sleep(10 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := bus.Publish(context.Background(), event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}