# Real-backend acceptance

From the repository root, with Go, Docker, Node 22.12+ and Playwright Chromium installed:

```sh
(cd web && npm ci && npm run build && npx playwright install chromium)
bash web/tests/support/real-backend.sh
```

The runner starts its own pgvector/PostgreSQL 16 container with a 1 GiB tmpfs data
mount, migrates it, and starts the production `pcas serve` and `pcas worker`
binaries. It serves `web/dist`; there is no browser API interception. It runs
`golden.spec.ts` three times without retries, then the four older backend specs
once. Set `PCAS_GOLDEN_PORT` to change the default localhost port 18097.

To select a smaller run:

```sh
bash web/tests/support/real-backend.sh npx playwright test tests/golden.spec.ts --grep G4
```

`internal/testsupport/golden` uses `httptest` servers for the OpenAI-compatible
model, Telegram Bot API, and encrypted Web Push endpoint. Rules match the current
secretary sentence (not conversation history), or assistant/extraction prompts.
They can delay a response or fail exactly one request. `/control` records requests
and queues Telegram updates. All fixture listeners bind to loopback.

Only `api.telegram.org:443` and `push.pcas.test:443` are accepted by the fixture's
CONNECT proxy; both terminate at its local TLS server. Its temporary test CA is
trusted only by the child processes. No production endpoint, credential or
container is used. The empty direct-ChatGPT directory enables the unauthenticated
settings-screen test without authorizing a real account.

G6 grants Chromium notification permission and records its actual value. In
headless Chromium on the acceptance host it remains `denied`; the permitted
fallback registers a valid test subscription through the real API and checks that
the local endpoint receives a nonempty `aes128gcm` request with VAPID authorization.
This proves backend delivery, not receipt by a browser push service or a phone.
The actual reminder timer is not accelerated. G9 withholds a model response for
95 seconds, exercising PCAS's 90-second secretary timeout.

Results, timings, screenshots, failure traces and service logs live under
`web/test-results/`; CI uploads them even on failure. Each run deletes its own
container (including anonymous volumes), private settings, binaries and fixture
CA on exit. Only recorded child PIDs are terminated. Real-model walkthroughs are
separate and require explicit user consent; this runner never enables them.
