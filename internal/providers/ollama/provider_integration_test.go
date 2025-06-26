// +build integration

package ollama

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestProvider_Execute_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Define the container request
	req := testcontainers.ContainerRequest{
		Image:        "ollama/ollama:latest",
		ExposedPorts: []string{"11434/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("11434/tcp"),
			wait.ForHTTP("/api/tags").WithPort("11434/tcp").WithStartupTimeout(2 * time.Minute),
		),
		Env: map[string]string{
			"OLLAMA_HOST": "0.0.0.0",
		},
	}

	// Start the container
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Ollama container: %v", err)
	}
	defer container.Terminate(ctx)

	// Get the host and port
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "11434")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	baseURL := fmt.Sprintf("http://%s:%s", host, port.Port())
	t.Logf("Ollama container running at %s", baseURL)

	// Pull a tiny model for testing
	// Note: In a real CI environment, you might want to build a custom image with the model pre-loaded
	t.Log("Pulling tinydolphin model...")
	pullCmd := []string{"ollama", "pull", "tinydolphin"}
	exitCode, _, err := container.Exec(ctx, pullCmd)
	if err != nil || exitCode != 0 {
		t.Fatalf("Failed to pull tinydolphin model: %v (exit code: %d)", err, exitCode)
	}

	// Create provider
	provider := NewProvider(nil, baseURL)

	// Test cases
	tests := []struct {
		name     string
		request  map[string]interface{}
		wantErr  bool
		errCheck func(error) bool
	}{
		{
			name: "successful inference",
			request: map[string]interface{}{
				"model":  "tinydolphin",
				"prompt": "Say hello in one word",
			},
			wantErr: false,
		},
		{
			name: "missing model",
			request: map[string]interface{}{
				"prompt": "Test",
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err != nil && contains(err.Error(), "missing required field: model")
			},
		},
		{
			name: "non-existent model",
			request: map[string]interface{}{
				"model":  "non-existent-model",
				"prompt": "Test",
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err != nil && contains(err.Error(), "provider internal error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := provider.Execute(ctx, tt.request)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errCheck != nil && !tt.errCheck(err) {
					t.Errorf("Error check failed: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if response == "" {
					t.Errorf("Expected non-empty response")
				} else {
					t.Logf("Model response: %s", response)
				}
			}
		})
	}
}