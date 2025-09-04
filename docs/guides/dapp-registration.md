---
title: "DApp Registration via Admin Events"
description: "How D-Apps can register event routes dynamically without editing policy.yaml."
tags: ["dapp", "policy", "admin", "registration"]
version: "0.1.3"
---

# DApp Registration via Admin Events

PCAS supports dynamic policy updates through admin control events. A D-App can register a new routing rule by publishing a special admin event; PCAS updates the in-memory policy immediately and persists changes to `policy.yaml`.

## Enable Admin API

Set an admin token when deploying PCAS (recommended):

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/soaringjerry/pcas/main/scripts/install-or-update.sh)" -- \
  --dir /opt/pcas --name pcas-instance --port 50051 \
  --admin-token my-secret
```

This sets `PCAS_ADMIN_TOKEN=my-secret` in the server container.

## Event Format

- `type`: `pcas.admin.policy.add_rule.v1`
- `attributes.admin_token`: must equal `PCAS_ADMIN_TOKEN` if configured
- `data` (JSON object):
  - `event_type` (string, required)
  - `provider` (string, required)
  - `prompt_template` (string, optional)
  - `name` (string, optional)

## Example (with pcasctl)

```bash
./bin/pcasctl emit \
  --server 127.0.0.1:50051 \
  --type pcas.admin.policy.add_rule.v1 \
  --data '{
    "event_type":"dapp.example.translate.v1",
    "provider":"openai-gpt4",
    "prompt_template":"You are a translator. {{.text}}"
  }'
```

Notes:
- Currently `pcasctl` does not expose attributes flags. For production, D-Apps should set `attributes.admin_token` in the published event to pass authorization. If you need CLI support, open an issue or PR.
- On success, the rule is added immediately and persisted to `policy.yaml`.

## Verifying

1. List rules locally:
   ```bash
   ./bin/pcasctl policy list
   ```
2. Trigger the newly registered event and observe provider routing.

## Failure Modes

- 401 Unauthorized: missing/invalid `admin_token` while server has `PCAS_ADMIN_TOKEN` set
- 400 Bad Request: missing `event_type` or `provider`; or malformed `data`

