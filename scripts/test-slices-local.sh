#!/usr/bin/env bash
# Runs the PostgreSQL package in N parallel slices against one test database.
# Each test uses its own schema, so slices do not share tables.
#   run-shards.sh <repo> <slices> <output dir> [test name regexp]
set -uo pipefail
cd "$1"; total=${2:-4}; out=$3; filter=${4:-^Test}
mkdir -p "$out"; rm -f "$out"/slice-*.log
go test -c -o "$out/postgres.test" ./internal/postgres || exit 2
names=$(cd internal/postgres && "$out/postgres.test" -test.list '^Test' | grep '^Test' | grep -E "$filter" | sort)
echo "tests: $(printf '%s\n' "$names" | wc -l)"
pids=()
for i in $(seq 0 $((total-1))); do
  picked=$(printf '%s\n' "$names" | awk -v i="$i" -v n="$total" '(NR - 1) % n == i' | paste -sd '|' -)
  [ -n "$picked" ] || continue
  ( cd internal/postgres && "$out/postgres.test" -test.count=1 -test.timeout 40m -test.run "^($picked)\$" > "$out/slice-$i.log" 2>&1; echo "exit=$?" >> "$out/slice-$i.log" ) &
  pids+=($!)
done
wait "${pids[@]}"
grep -H '^exit=' "$out"/slice-*.log
grep -h '^--- FAIL\|^FAIL\|^panic:' "$out"/slice-*.log | sort | uniq -c | head -60
