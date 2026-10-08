# PCAS Web

Frontend implementation reference. Updated 2026-10-08.

React and TypeScript provide the interface. The Go API stores business data in PostgreSQL.
Login uses an HttpOnly session cookie.
Product requirements are in the [Whitepaper](../docs/whitepaper.md).
Presentation rules are in the [Interface Principles](../docs/design/principles.md).

## Development

Use the Node version necessary by [package.json](package.json).
Start the Go API at `127.0.0.1:8090` for the default development proxy.
From `web/`, run:

```sh
npm ci
npm run dev
```

Vite proxies `/v1` to the API. In production, Go serves `dist`.
See [Deployment](../docs/deployment.md) for credentials and model settings.

## Implementation References

| Area | Reference |
|---|---|
| Home sections and time calculations | [Hall model](src/domain/hall.ts). |
| Workspace memory, files, and documents | [Workspace store](src/store/studio.ts). |
| Commands and persisted state | [Store code](src/store/). |
| Interface preview data | [Agent preview](src/domain/agent.ts). Server context and access checks apply to executed work. |
| Browser notifications | [Service worker](public/sw.js). It handles notifications without a page cache. |
| Application installation | [Manifest](public/manifest.webmanifest). |

Commands use `requestId` and the applicable revision checks.
Only a successful server response marks data as saved. After a conflict, load the current state.
Undo checks its recorded dependencies. A repeated request must not execute again.
See [Service Reference](../docs/memory-service.md) for server behavior.

The workspace time zone controls home grouping and timelines, including daylight-saving changes.
On first login, a new workspace uses the browser's IANA time zone when supplied.
A client without a time zone uses UTC. Browser login does not replace an existing workspace setting.

## Verification

From `web/`, run the checks that apply to the change:

```sh
npm run lint
npm run type-check
npm run build
```

For browser replay, use a temporary test database and test service:

```sh
env -u DISPLAY npx playwright install chromium
env -u DISPLAY \
  PCAS_TEST_BASE_URL=http://127.0.0.1:18090 \
  PCAS_TEST_API_TOKEN=test-only-secret-at-least-32-characters \
  npx playwright test
```

For acceptance with the actual backend, run from the repository root:

```sh
env -u DISPLAY bash web/tests/support/real-backend.sh
```

The [Backend Test Guide](tests/support/README.md) gives setup, fixtures, evidence limits, and cleanup behavior.
Fixture-model acceptance and real-model acceptance have different evidence.
Use the [Development Workflow](../docs/tasks/process.md#3-verification) for real-model checks.
