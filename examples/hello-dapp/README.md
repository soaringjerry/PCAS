# Hello D-App Example

This is a complete working example of the Hello D-App from the tutorial.

## Quick Start

1. **Start PCAS** (from the PCAS root directory):
   ```bash
   make dev-up
   ```

2. **Run this D-App**:
   ```bash
   go run .
   ```

3. **Emit a test event** (from the PCAS root directory):
   ```bash
   ./bin/pcasctl emit --type "pcas.memory.create.v1" --subject "Hello from D-App!"
   ```

4. **See the results** in the D-App terminal!

## What This Example Shows

- How to connect to PCAS using the SDK
- How to subscribe to all events
- How to process and display events
- Proper error handling and graceful shutdown

## Customization

You can modify this example to:
- Filter for specific event types
- Process events differently based on their content
- Emit new events in response to received events
- Add persistence or external integrations