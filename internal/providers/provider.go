package providers

import (
	"context"
)

// ComputeProvider is the interface that all compute providers must implement
type ComputeProvider interface {
	// Execute processes a request and returns a response
	Execute(ctx context.Context, requestData map[string]interface{}) (string, error)
}

// StreamingComputeProvider is the interface for providers that support streaming
type StreamingComputeProvider interface {
	ComputeProvider
	// ExecuteStream handles bidirectional streaming
	ExecuteStream(ctx context.Context, attributes map[string]string, input <-chan []byte, output chan<- []byte) error
}