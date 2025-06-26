package bus

// IsFactEvent determines if an event type represents a fact/memory that should be vectorized
// This function is used by both the server (for real-time vectorization) and the backfill command
func IsFactEvent(eventType string) bool {
	// Whitelist of event types that represent facts/memories
	factEventTypes := []string{
		"pcas.memory.create.v1",
		"user.note.v1",
		"user.reminder.v1",
		"user.task.v1",
		"user.memory.v1",
	}
	
	for _, factType := range factEventTypes {
		if eventType == factType {
			return true
		}
	}
	
	return false
}