package mock

import (
	"context"
	"testing"
)

func TestProvider_Execute(t *testing.T) {
	// Create a new MockProvider instance
	provider := NewProvider()
	
	// Create a test context
	ctx := context.Background()
	
	// Create test request data
	requestData := map[string]interface{}{
		"test": "data",
		"key":  "value",
	}
	
	// Call Execute method
	response, err := provider.Execute(ctx, requestData)
	
	// Check that there's no error
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	
	// Check that the response matches our expected fixed value
	expectedResponse := "Mock response from mock-provider"
	if response != expectedResponse {
		t.Errorf("Execute returned %q, expected %q", response, expectedResponse)
	}
}

func TestProvider_Execute_EmptyRequest(t *testing.T) {
	// Test with empty request data
	provider := NewProvider()
	ctx := context.Background()
	requestData := map[string]interface{}{}
	
	response, err := provider.Execute(ctx, requestData)
	
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	
	expectedResponse := "Mock response from mock-provider"
	if response != expectedResponse {
		t.Errorf("Execute returned %q, expected %q", response, expectedResponse)
	}
}

func TestProvider_Execute_NilRequest(t *testing.T) {
	// Test with nil request data
	provider := NewProvider()
	ctx := context.Background()
	
	response, err := provider.Execute(ctx, nil)
	
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	
	expectedResponse := "Mock response from mock-provider"
	if response != expectedResponse {
		t.Errorf("Execute returned %q, expected %q", response, expectedResponse)
	}
}