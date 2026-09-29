.PHONY: fmt fmt-check lint test test-integration build check

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

lint:
	go vet ./...

test:
	go test -race ./...

test-integration:
	@test -n "$$PCAS_TEST_DATABASE_URL" || (echo 'PCAS_TEST_DATABASE_URL is required'; exit 1)
	go test -race -count=1 ./internal/postgres

build:
	go build -trimpath -o bin/pcas ./cmd/pcas

check: fmt-check lint test build
