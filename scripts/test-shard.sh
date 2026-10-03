#!/usr/bin/env bash
# Runs one slice of the PostgreSQL integration tests so CI can run the slices
# side by side, each against its own database. Every top-level test lands in
# exactly one slice; `make check` still runs them all in one go.
#   scripts/test-shard.sh <index> <total>     index counts from 0
set -euo pipefail
index=${1:?slice index}
total=${2:?slice count}
package=./internal/postgres
names=$(go test -list '^Test' "$package" | grep '^Test' | sort)
[ -n "$names" ] || { echo "no tests listed in $package" >&2; exit 1; }
picked=$(printf '%s\n' "$names" | awk -v i="$index" -v n="$total" '(NR - 1) % n == i')
[ -n "$picked" ] || { echo "slice $index of $total is empty" >&2; exit 1; }
echo "slice $index/$total: $(printf '%s\n' "$picked" | wc -l) of $(printf '%s\n' "$names" | wc -l) tests"
pattern="^($(printf '%s\n' "$picked" | paste -sd '|' -))\$"
go test -race -count=1 -timeout 30m -run "$pattern" "$package"
