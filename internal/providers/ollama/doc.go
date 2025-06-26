// Package ollama provides a ComputeProvider implementation for running
// local language models through the Ollama API.
//
// This provider enables PCAS to leverage locally-hosted LLMs, ensuring
// complete data privacy and eliminating dependency on cloud services.
//
// Usage:
//
//	httpClient := &http.Client{Timeout: 30 * time.Second}
//	provider := ollama.NewProvider(httpClient, "http://localhost:11434")
//	
//	response, err := provider.Execute(ctx, map[string]interface{}{
//	    "model": "llama3:8b",
//	    "prompt": "Explain quantum computing in simple terms",
//	})
//
// The provider requires the model to be explicitly specified in the request.
// Common models include:
//   - llama3:8b - Llama 3 8B parameter model (recommended for general use)
//   - llama3:70b - Llama 3 70B parameter model (higher quality, more resources)
//   - mistral - Mistral 7B model
//   - tinydolphin - Tiny model suitable for testing
//
// The provider implements automatic retry logic for transient failures and
// includes comprehensive error handling with standardized error types.
package ollama