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
   - Start ChromaDB on port 8000
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

1. **ChromaDB**: Vector database for semantic search
   - Runs on port 8000
   - Data persisted in `./data/chroma`
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

- ChromaDB data is stored in `./data/chroma`
- This directory is gitignored
- Data persists between container restarts
- Use `make dev-clean` to remove all data

## Troubleshooting

1. **Port conflicts**: If ports 8000 or 50051 are already in use, stop the conflicting services or modify `docker-compose.yml`

2. **Build failures**: Ensure you've run `go mod vendor` to populate the vendor directory

3. **ChromaDB connection issues**: Check that ChromaDB is healthy:
   ```bash
   curl http://localhost:8000/api/v1/heartbeat
   ```

4. **Hot reload not working**: Check Air logs in the PCAS container output