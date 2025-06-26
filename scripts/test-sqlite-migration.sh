#!/bin/bash

echo "=== SQLite Migration Test Script ==="
echo

echo "1. Testing Pure Go SQLite implementation (default)..."
go test -v ./internal/storage/sqlite/... || exit 1
echo "✅ Pure Go SQLite tests passed"
echo

echo "2. Testing CGO SQLite implementation..."
CGO_ENABLED=1 go test -tags cgo -v ./internal/storage/sqlite/... || exit 1
echo "✅ CGO SQLite tests passed"
echo

echo "3. Building with Pure Go (production mode)..."
go build -mod=vendor -tags 'netgo' -o /tmp/pcas-pure ./cmd/pcas || exit 1
echo "✅ Pure Go build successful"
echo

echo "4. Building with CGO (if needed for comparison)..."
CGO_ENABLED=1 go build -mod=vendor -tags 'cgo' -o /tmp/pcas-cgo ./cmd/pcas || exit 1
echo "✅ CGO build successful"
echo

echo "5. Checking binary sizes..."
ls -lh /tmp/pcas-* | awk '{print $9, $5}'
echo

echo "=== All tests passed! ==="
echo
echo "The SQLite migration is complete. By default, PCAS now uses the pure Go"
echo "SQLite implementation (modernc.org/sqlite), which eliminates CGO dependencies."
echo
echo "To use the CGO version (if needed):"
echo "  CGO_ENABLED=1 go build -tags cgo ..."
echo
echo "To use the pure Go version (default):"
echo "  go build ..."