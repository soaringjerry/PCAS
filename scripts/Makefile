.PHONY: proto lint test

proto:
	buf generate

lint:
	golangci-lint run ./...

test:
	go test ./...