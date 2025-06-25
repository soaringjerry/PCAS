# Find buf, searching local bin and system PATH
BUF := $(shell command -v buf 2>/dev/null)

# Default target
all: build

.PHONY: all proto lint test build clean

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
	go build -mod=vendor -v -o bin/pcas ./cmd/pcas
	go build -mod=vendor -v -o bin/pcasctl ./cmd/pcasctl

test: ## Run all tests
	@echo "--> Running tests..."
	go test -mod=vendor -v ./...

lint: ## Lint all Go files
	@echo "--> Linting files..."
	golangci-lint run

##@ Housekeeping
clean: ## Clean up build artifacts and runtime data
	@echo "--> Cleaning up..."
	@rm -rf bin/
	@rm -rf data/
