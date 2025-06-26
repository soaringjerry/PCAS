# Ollama Provider Documentation

The Ollama provider enables PCAS to use locally-hosted Large Language Models (LLMs) through the Ollama API, ensuring complete data privacy and eliminating cloud dependencies.

## Prerequisites

1. Install Ollama: https://ollama.ai
2. Pull a model (e.g., `ollama pull llama3:8b`)
3. Ensure Ollama is running (default: http://localhost:11434)

## Configuration

Add the Ollama provider to your `policy.yaml`:

```yaml
providers:
  - name: ollama-llama3
    type: ollama
    # host: ${OLLAMA_HOST} # defaults to http://localhost:11434
```

## Usage

### Via pcasctl

Send events with the local LLM event type:

```bash
# Using the default llama3:8b model
./bin/pcasctl emit \
  --type "pcas.user.prompt.local.v1" \
  --data '{"model": "llama3:8b", "prompt": "Explain quantum computing"}'
```

### Via SDK

```go
client.Emit(ctx, &eventsv1.Event{
    Type: "pcas.user.prompt.local.v1",
    Data: map[string]interface{}{
        "model": "llama3:8b",
        "prompt": "Your prompt here",
    },
})
```

## Supported Models

| Model | Size | Use Case |
|-------|------|----------|
| llama3:8b | ~4.5GB | General purpose, balanced performance |
| llama3:70b | ~40GB | Higher quality, requires more resources |
| mistral | ~4GB | Fast, good for code |
| tinydolphin | ~0.5GB | Testing and development |

## Features

- **Automatic Retry**: Retries up to 2 times for transient failures
- **Timeout Handling**: Default 30-second timeout per request
- **Parameter Validation**: Ensures required fields are present
- **Structured Logging**: Tracks execution time and errors

## Testing

### Unit Tests
```bash
go test ./internal/providers/ollama/...
```

### Integration Tests
Requires Docker:
```bash
go test -tags=integration ./internal/providers/ollama/...
```

## Error Handling

The provider returns standardized errors:

- `ErrInvalidInput`: Missing required fields (model, prompt)
- `ErrProviderUnavailable`: Ollama service unreachable
- `ErrTimeout`: Request exceeded timeout
- `ErrInternalError`: Unexpected errors

## Performance Considerations

1. **Model Loading**: First request may be slow as model loads into memory
2. **Resource Usage**: Monitor RAM usage, especially for larger models
3. **Concurrent Requests**: Ollama handles requests sequentially by default

## Troubleshooting

### Ollama Not Responding
```bash
# Check if Ollama is running
curl http://localhost:11434/api/tags

# Restart Ollama
ollama serve
```

### Model Not Found
```bash
# List available models
ollama list

# Pull required model
ollama pull llama3:8b
```

### Slow Response Times
- Use smaller models for faster responses
- Ensure sufficient RAM (8GB+ for llama3:8b)
- Check CPU usage during inference