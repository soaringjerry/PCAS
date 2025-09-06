---
title: "Event‑Driven Transcription D‑App Integration"
description: "How to integrate an external transcription service (e.g., DreamTrans) with PCAS via the Event Bus."
tags: ["dapp", "transcription", "event-bus", "integration", "guide"]
version: "0.1.0"
---

# Event‑Driven Transcription D‑App Integration

This guide describes how to integrate an external transcription service (e.g., DreamTrans) as a D‑App using PCAS's event‑driven model. The D‑App connects to the PCAS gRPC Event Bus, subscribes to request events, performs the transcription, and publishes response events back to PCAS.

PCAS does not require the D‑App to host a gRPC server or listen on TCP ports. The D‑App is a gRPC client of PCAS (default: `127.0.0.1:50051` or your server address).

## High‑Level Flow

1) PCAS publishes a transcription request event and broadcasts it to subscribers.
2) Your D‑App is subscribed to the bus, filters for the request type, and performs transcription.
3) Your D‑App publishes a response event with `correlation_id` pointing to the original request `id`.
4) PCAS stores the response event and broadcasts it to subscribers (including the original caller if it is listening).

## Event Types

Use the following event types (you can change the prefix to match your org, but keep the structure):

- Request: `capability.audio.transcribe.request.v1`
- Response: `capability.audio.transcribe.response.v1`
- Error (optional): `capability.audio.transcribe.error.v1`

## Request Event (PCAS → D‑App)

- `type`: `capability.audio.transcribe.request.v1`
- Core fields
  - `id`: globally unique ID (set by PCAS)
  - `trace_id`: tracing ID (propagated along the chain)
  - `user_id` (optional): end‑user context
  - `session_id` (optional): logical session
  - `attributes` (optional): key/value metadata
    - Recommended keys: `language`, `format`, `sample_rate`, `source`
- `data` (one of)
  - `audio_base64` (string): base64‑encoded audio payload
  - `audio_url` (string): where to fetch the audio (preferred for large media)

Example JSON (data payload only):

```json
{
  "audio_base64": "<base64-bytes>",
  "language": "en",
  "format": "wav",
  "sample_rate": 16000
}
```

Notes:
- PCAS broadcasts inbound request events; your D‑App will receive them after subscribing.
- For large audio, prefer `audio_url` and have the D‑App fetch the content.

## Response Event (D‑App → PCAS)

- `type`: `capability.audio.transcribe.response.v1`
- Core fields
  - `correlation_id`: MUST equal the original request event `id`
  - `trace_id`: SHOULD copy the original `trace_id`
  - `user_id`/`session_id`: MAY be copied for filtering/analytics
  - `source`: set to your D‑App identifier, e.g., `dapp.dreamtrans`
- `data`
  - `text` (string): transcription result (required)
  - `language` (string, optional)
  - `segments` (array, optional): detailed segmentation if available

Example JSON (data payload only):

```json
{
  "text": "Hello world, this is a demo.",
  "language": "en"
}
```

## Error Event (optional)

- `type`: `capability.audio.transcribe.error.v1`
- `correlation_id`: original request `id`
- `data`: `{ "code": "...", "message": "..." }`

## Best‑Practice Policy

To keep PCAS from handling transcription itself (so your D‑App does it), DO NOT route the request type to an internal provider in `policy.yaml`.

```yaml
# policy.yaml (excerpt)
providers:
  - name: mock-provider
    type: mock

rules:
  # Intentionally no rule routing capability.audio.transcribe.request.v1
  # so PCAS broadcasts it and your D-App processes it.
```

## Minimal Go Example (Subscribe + Respond)

This sample uses the generated gRPC client directly for full control of `correlation_id` and `trace_id`.

```go
package main

import (
  "context"
  "encoding/base64"
  "log"
  "time"

  "google.golang.org/grpc"
  "google.golang.org/grpc/credentials/insecure"
  "google.golang.org/protobuf/types/known/anypb"
  "google.golang.org/protobuf/types/known/structpb"
  "google.golang.org/protobuf/types/known/timestamppb"

  busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
  eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func main() {
  addr := "127.0.0.1:50051" // PCAS server address
  conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
  if err != nil { log.Fatal(err) }
  defer conn.Close()

  client := busv1.NewEventBusServiceClient(conn)

  // Subscribe to events
  sub, err := client.Subscribe(context.Background(), &busv1.SubscribeRequest{ClientId: "dreamtrans-dapp"})
  if err != nil { log.Fatal(err) }

  for {
    evt, err := sub.Recv()
    if err != nil { log.Fatal(err) }
    if evt.GetType() != "capability.audio.transcribe.request.v1" { continue }

    // Extract audio
    var audioB64 string
    var language string
    if evt.Data != nil {
      val := &structpb.Value{}
      if evt.Data.UnmarshalTo(val) == nil {
        if m, ok := val.AsInterface().(map[string]interface{}); ok {
          if s, ok := m["audio_base64"].(string); ok { audioB64 = s }
          if s, ok := m["language"].(string); ok { language = s }
        }
      }
    }
    if audioB64 == "" { log.Println("missing audio_base64; skipping"); continue }

    // Decode and transcribe (replace with real DreamTrans call)
    audioBytes, _ := base64.StdEncoding.DecodeString(audioB64)
    _ = audioBytes // use bytes in your transcription API
    text := "<transcribed text>" // TODO: call DreamTrans here

    // Build response data
    respMap := map[string]interface{}{"text": text, "language": language}
    respVal, _ := structpb.NewValue(respMap)
    respAny, _ := anypb.New(respVal)

    // Publish response event
    resp := &eventsv1.Event{
      Id:          "", // let server assign or generate your own UUID
      Type:        "capability.audio.transcribe.response.v1",
      Source:      "dapp.dreamtrans",
      Specversion: "1.0",
      Time:        timestamppb.Now(),
      TraceId:     evt.GetTraceId(),
      CorrelationId: evt.GetId(),
      UserId:      evt.GetUserId(),
      SessionId:   evt.GetSessionId(),
      Data:        respAny,
    }
    if _, err := client.Publish(context.Background(), resp); err != nil {
      log.Printf("publish response failed: %v", err)
    }
  }
}
```

## Operational Notes

- Delivery semantics: subscription is a live stream; if disconnected, events may be missed. Keep your D‑App online and handle reconnects.
- Backpressure: PCAS uses buffered channels; if your D‑App is slow, older events may drop. Consume promptly.
- Large media: for large audio, prefer `audio_url` to avoid bloating event payloads.
- Correlation: always set `correlation_id` on responses; a client can match it to the original request.

## Testing Locally

1) Start PCAS (compose or binary).
2) Run your D‑App.
3) Publish a test request from anywhere:

```bash
./bin/pcasctl emit \
  --type capability.audio.transcribe.request.v1 \
  --data '{"audio_base64":"<base64 sample>", "language":"en"}'
```

4) Observe your D‑App logs and the broadcast response event. You can also run a second subscriber to verify response delivery.

