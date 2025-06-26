package providers

import (
	"context"
)

// ComputeProvider is the interface that all compute providers must implement
type ComputeProvider interface {
	// Execute processes a request and returns a response
	Execute(ctx context.Context, requestData map[string]interface{}) (string, error)
}