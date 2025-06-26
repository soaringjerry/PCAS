# Find buf, searching local bin and system PATH
BUF := $(shell command -v buf 2>/dev/null)

# Default target
all: build

.PHONY: all proto lint test build clean help dev-up dev-down dev-logs dev-clean

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Generation
proto: check-buf ## Generate protobuf code
	@echo "--> Generating protobuf files..."
	$(BUF) generate

check-buf:
	@if [ -z "$(BUF)" ]; then \
		echo "Error: 'buf' command not found."; \
		echo "Please install it by following the instructions at https://buf.build/docs/installation"; \
		exit 1; \
	fi

##@ Development
build: ## Build all binaries
	@echo "--> Building binaries..."
	@mkdir -p bin
	go build -tags netgo -v -o bin/pcas ./cmd/pcas
	go build -tags netgo -v -o bin/pcasctl ./cmd/pcasctl

test: ## Run all tests
	@echo "--> Running tests..."
	go test -v ./...

lint: ## Lint all Go files
	@echo "--> Linting files..."
	golangci-lint run

##@ Docker Development
dev-up: ## Start development environment with Docker Compose
	@echo "--> Starting development environment..."
	@mkdir -p data/postgres
	docker-compose --compatibility up --build --force-recreate $(DOCKER_ARGS)

dev-down: ## Stop development environment
	@echo "--> Stopping development environment..."
	docker-compose down

dev-logs: ## Show logs from development environment
	docker-compose logs -f

dev-clean: ## Clean up development environment including volumes
	@echo "--> Cleaning development environment..."
	docker-compose down -v
	@rm -rf data/

##@ Housekeeping
clean: ## Clean up build artifacts and runtime data
	@echo "--> Cleaning up..."
	@rm -rf bin/
	@rm -rf data/
