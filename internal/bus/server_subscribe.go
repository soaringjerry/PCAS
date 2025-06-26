package bus

import (
	"log"

	busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Subscribe handles streaming events to clients
func (s *Server) Subscribe(req *busv1.SubscribeRequest, stream busv1.EventBusService_SubscribeServer) error {
	clientID := req.ClientId
	log.Printf("Client %s subscribing to events", clientID)
	
	// Create a channel for this client
	eventChan := make(chan *eventsv1.Event, 100)
	
	// Register the subscriber
	s.subMutex.Lock()
	s.subscribers[clientID] = eventChan
	s.subMutex.Unlock()
	
	// Ensure cleanup on disconnect
	defer func() {
		s.subMutex.Lock()
		delete(s.subscribers, clientID)
		s.subMutex.Unlock()
		close(eventChan)
		log.Printf("Client %s unsubscribed", clientID)
	}()
	
	// Stream events to the client
	for {
		select {
		case event := <-eventChan:
			if err := stream.Send(event); err != nil {
				log.Printf("Error sending event to client %s: %v", clientID, err)
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// broadcastEvent sends an event to all subscribers
func (s *Server) broadcastEvent(event *eventsv1.Event) {
	s.subMutex.RLock()
	defer s.subMutex.RUnlock()
	
	log.Printf("Broadcasting event %s to %d subscribers", event.Id, len(s.subscribers))
	
	for clientID, eventChan := range s.subscribers {
		select {
		case eventChan <- event:
			// Event sent successfully
		default:
			// Channel is full, skip this client
			log.Printf("Warning: Event channel full for client %s, skipping event", clientID)
		}
	}
}