package mock

import (
	"context"
	"fmt"
)

// Provider is a mock implementation of ComputeProvider for testing
type Provider struct{}

// NewProvider creates a new mock provider instance
func NewProvider() *Provider {
	return &Provider{}
}

// Execute implements the ComputeProvider interface
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
	// Return a hardcoded response with the request data
	return fmt.Sprintf("Mock response for request: %v", requestData), nil
}