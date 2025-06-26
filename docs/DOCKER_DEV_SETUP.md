# Docker Development Setup

This guide explains how to use Docker Compose to quickly set up a development environment for PCAS.

## Prerequisites

- Docker and Docker Compose installed
- Make command available
- OpenAI API key (if using OpenAI provider)

## Quick Start

1. **Copy the environment file**:
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and add your OpenAI API key if needed.

2. **Start the development environment**:
   ```bash
   make dev-up
   ```
   This will:
   - Start PostgreSQL with pgvector extension on port 5432
   - Build and start PCAS with hot reload on port 50051
   - Create necessary data directories

3. **Check logs** (in another terminal):
   ```bash
   make dev-logs
   ```

4. **Test the setup**:
   ```bash
   # In another terminal
   ./bin/pcasctl ping
   ```

## Available Commands

- `make dev-up` - Start all services
- `make dev-down` - Stop all services
- `make dev-logs` - View logs from all services
- `make dev-clean` - Stop services and clean up data

## How It Works

### Services

1. **PostgreSQL with pgvector**: Vector database for semantic search
   - Runs on port 5432
   - Uses the `pgvector/pgvector:pg16` image
   - Data persisted in Docker named volume `postgres_data`
   - Health checks ensure it's ready before PCAS starts

2. **PCAS**: The main application server
   - Runs on port 50051 (gRPC)
   - Uses Air for hot reload - changes to Go files automatically rebuild
   - Mounts source code for live development

### Hot Reload

The development container uses [Air](https://github.com/cosmtrek/air) to watch for file changes and automatically rebuild/restart the server. Any changes to:
- `.go` files
- `.proto` files
- `.yaml` files

Will trigger a rebuild and restart.

### Data Persistence

- PostgreSQL data is stored in Docker named volume `postgres_data`
- Data persists between container restarts
- Use `make dev-clean` to remove all data (including the volume)

## Troubleshooting

1. **Port conflicts**: If ports 5432 or 50051 are already in use, stop the conflicting services or modify `docker-compose.yml`

2. **Build failures**: The container handles all dependencies automatically - no need to run `go mod vendor`

3. **PostgreSQL connection issues**: Check that PostgreSQL is healthy:
   ```bash
   docker-compose ps postgres
   ```

4. **Hot reload not working**: Check Air logs in the PCAS container output