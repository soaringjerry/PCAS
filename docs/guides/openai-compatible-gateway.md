---
title: "OpenAI-Compatible Gateway with Auto-RAG"
description: "Expose OpenAI-compatible endpoints that automatically inject PCAS memories (RAG) by user_id."
tags: ["gateway", "openai", "rag", "api"]
version: "0.1.0"
---

# OpenAI-Compatible Gateway with Auto‑RAG

This gateway exposes OpenAI-compatible endpoints and auto-injects user memories from PCAS as context (RAG). Clients can keep using standard OpenAI SDKs by changing only the base URL and adding a `user_id`.

## Endpoints (minimal)

- `POST /v1/chat/completions` (non-streaming)
- `POST /v1/embeddings` (pass-through)

Default bind: `127.0.0.1:50052` (configurable).

## How It Works

1. Client calls `/v1/chat/completions` with standard OpenAI payload.
2. Gateway reads `user_id` (from request JSON or header `X-User-ID`).
3. Gateway queries PCAS via gRPC Search for the most relevant segments/memories for that user.
4. Gateway injects the retrieved context as a system message ahead of user messages.
5. Gateway calls the upstream model (OpenRouter/OpenAI/local) via go-openai and returns the standard OpenAI response.

No client changes required beyond base URL and user identification.

## User Identification and RAG Controls

- `user_id`: string
  - in JSON payload (top-level), or
  - in header `X-User-ID`
- `pcas_rag`: boolean or object (optional)
  - when truthy, the gateway performs RAG
  - object form may include `{ "top_k": 5, "filters": { "course": "math101" } }` (best-effort)

## Request Examples

Chat Completions (RAG on):

```json
POST /v1/chat/completions
{
  "model": "gpt-4o-mini",
  "user_id": "alice",
  "pcas_rag": true,
  "messages": [
    {"role": "user", "content": "Explain topic X using my notes."}
  ]
}
```

Embeddings (pass-through):

```json
POST /v1/embeddings
{
  "model": "text-embedding-3-large",
  "input": ["hello world"]
}
```

## Environment

- `PCAS_ADDR` (default `127.0.0.1:50051`) – gRPC address of PCAS Event Bus Search API
- `PCAS_GW_ADDR` (default `127.0.0.1:50052`) – HTTP bind
- `OPENAI_API_KEY` – upstream key (OpenRouter/OpenAI)
- `OPENAI_BASE_URL` – upstream base (auto-detects OpenRouter if key starts with `sk-or-`)

## Run

```bash
pcas gateway --addr 127.0.0.1:50052 --pcas 127.0.0.1:50051
```

Point your OpenAI client to `http://127.0.0.1:50052/v1`, include `user_id` and optional `pcas_rag`.

## Notes

- This is a minimal non-streaming implementation. Streaming can be added later.
- Context comes from PCAS memories and indexable segments (by `user_id` and optional attribute filters).
- To enrich personal context, send indexable segments (`resource.segment.v1` with `attributes.index=true`) and personal memories (`pcas.memory.create.v1`) to PCAS.

