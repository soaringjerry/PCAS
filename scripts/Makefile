.PHONY: check-buf proto lint test build clean

check-buf:
	@which buf > /dev/null 2>&1 || (echo "Error: 'buf' command not found. Please install buf from https://buf.build/docs/installation" && exit 1)

proto: check-buf
	buf generate

lint:
	golangci-lint run -mod=vendor ./...

test:
	go test -mod=vendor ./...

build: check-buf
	@mkdir -p bin
	go build -mod=vendor -o bin/pcas ./cmd/pcas
	go build -mod=vendor -o bin/pcasctl ./cmd/pcasctl

clean:
	rm -rf bin/ data/ *.log logs/ gen/
	@echo "Cleaned build artifacts and runtime data"