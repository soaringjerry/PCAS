package mock

import (
	"context"
)

// Provider is a mock implementation of ComputeProvider for testing
type Provider struct{}

// NewProvider creates a new mock provider instance
func NewProvider() *Provider {
	return &Provider{}
}

// Execute implements the ComputeProvider interface
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
	// Return a fixed, predictable response
	return "Mock response from mock-provider", nil
}