---
title: "Installation Guide"
description: "A comprehensive guide to installing and setting up PCAS on your local machine."
tags: ["installation", "getting-started", "setup"]
version: "0.1.2"
---

# Installation Guide

Welcome to the PCAS installation guide! This document will walk you through the steps to get PCAS up and running on your system.

## Prerequisites

Before you begin, ensure you have the following installed:

-   **Go**: Version 1.24 or later. You can download it from [golang.org](https://golang.org/dl/).
-   **Docker & Docker Compose**: Required for running PCAS with its default PostgreSQL and ChromaDB dependencies. Follow the official Docker installation guide for your operating system.
-   **Git**: For cloning the PCAS repository.

## Step 1: Clone the PCAS Repository

Open your terminal and clone the PCAS repository:

```bash
git clone https://github.com/soaringjerry/PCAS.git
cd PCAS
```

## Step 2: Set Up Environment Variables

Copy the example environment file and configure your settings. If you plan to use OpenAI or other cloud providers, add your API keys here.

```bash
cp .env.example .env
# Open .env in your editor and add necessary API keys
```

## Step 3: Start PCAS with Docker Compose

PCAS uses Docker Compose to manage its services (PostgreSQL with pgvector, and the PCAS application itself).

```bash
make dev-up
```

This command will:
-   Pull necessary Docker images.
-   Build the PCAS application container.
-   Start PostgreSQL with pgvector on port `5432`.
-   Start the PCAS application with hot reload on port `50051` (gRPC).
-   Create necessary data directories for persistence.

Wait for all services to be ready. You can check the logs in another terminal:

```bash
make dev-logs
```

## Step 4: Verify Installation

Once all services are running, you can verify the installation using the `pcasctl` command-line tool:

```bash
./bin/pcasctl ping
```

You should see a successful response indicating that PCAS is running and responsive.

## Troubleshooting

-   **Port Conflicts**: If ports `5432` or `50051` are already in use, stop the conflicting services or modify `docker-compose.yml`.
-   **Build Failures**: Ensure you have Go installed correctly and that Docker is running.
-   **Service Not Responding**: Check Docker logs (`make dev-logs`) for errors.

## Next Steps

Now that PCAS is installed, you can:
-   Explore the [Hello DApp Tutorial](./hello-dapp-tutorial.md) to build your first application.
-   Dive into the [Architecture Overview](../architecture/pcas-overview.md) to understand PCAS's design principles.
-   Learn about specific integrations in the [Guides](../guides/).