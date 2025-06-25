# Logger D-App Example

This is a simple example D-App that demonstrates how to use the PCAS Go SDK.

## Features

- Connects to PCAS server
- Subscribes to the event stream
- Sends heartbeat events every 5 seconds
- Logs all events received from the event bus

## Running the Example

1. First, make sure PCAS server is running:
   ```bash
   go run ./cmd/pcas serve
   ```

2. In a new terminal, run the Logger D-App:
   ```bash
   go run ./examples/logger-dapp
   ```

3. (Optional) In another terminal, send test events:
   ```bash
   go run ./cmd/pcasctl emit -t "test.event.v1" --data '{"message": "Hello from pcasctl!"}'
   ```

## Configuration

- `PCAS_SERVER`: Set this environment variable to override the default server address (localhost:50051)

## What to Expect

- The app will log every event it receives from PCAS
- You'll see heartbeat responses every 5 seconds
- Any events emitted by other clients will also be logged

This demonstrates the power of PCAS's event-driven architecture and how easy it is to build reactive D-Apps using the SDK.