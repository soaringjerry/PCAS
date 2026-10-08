# PCAS

PCAS is a self-hosted personal AI support team.
The secretary, deputy, butler, and project workspaces use one memory core.
The user states a need. The team finds the necessary information, does permitted work, and shows the result.

[Documentation](docs/README.md) is the entry point for current specifications and historical records.
Start with the [Whitepaper](docs/whitepaper.md), [Memory Architecture](docs/memory-architecture.md), and [Project Status](docs/status.md).

## Run

Use Docker Compose. Copy `.env.example` to `.env`.
Set a separate database password, owner UUID, and random API token with at least 32 characters.

```sh
chmod 600 .env
docker compose build api
docker compose up -d --no-build
```

The default address is `http://127.0.0.1:12352`.
The login password is `PCAS_API_TOKEN`.
The default text channel uses the configured Codex account and `gpt-6.1-sol`.
API models, embeddings, and audio have separate configuration.
See [Deployment](docs/deployment.md) and [Input Reference](docs/connectors.md).

## Check

Use the Go version in `go.mod` and the Node.js version range in `web/package.json`.

```sh
make check
PCAS_TEST_DATABASE_URL='postgres://user:password@localhost/test?sslmode=disable' make test-integration
cd web
npm ci
npm run lint
npm run type-check
npm run build
```

The integration database needs pgvector and must be a disposable test database.
Without its environment variable, PostgreSQL integration tests can skip.
Run Playwright and Chromium with `env -u DISPLAY`.
See [Frontend Tests](web/README.md) for the applicable commands and isolated backend setup.
[Project Status](docs/status.md) states the limits of recorded model and live checks.

## License

Copyright (C) 2026 CoYume Pty Ltd (Australia).

PCAS uses the [GNU Affero General Public License v3.0](LICENSE).
The license defines source-distribution requirements for modified software offered over a network.
