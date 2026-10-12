#!/usr/bin/env bash
# Runs one slice of the PostgreSQL integration tests so CI can run the slices
# side by side, each against its own database. Every top-level test lands in
# exactly one slice; `make check` still runs them all in one go.
#   scripts/test-shard.sh <index> <total>     index counts from 0
#
# The slices are balanced by time. scripts/test-durations.txt gives the
# seconds of each test in a full CI run; the longest test goes first, each
# to the slice that is lightest so far. A test that the file does not know
# counts as five seconds. Refresh the file from the logs of a full run with
# scripts/test-durations.sh when the slices drift apart.
set -euo pipefail
index=${1:?slice index}
total=${2:?slice count}
package=./internal/postgres
durations=$(dirname "$0")/test-durations.txt
names=$(go test -list '^Test' "$package" | grep '^Test' | sort)
[ -n "$names" ] || { echo "no tests listed in $package" >&2; exit 1; }
picked=$(printf '%s\n' "$names" | awk -v i="$index" -v n="$total" -v file="$durations" '
  BEGIN { while ((getline line < file) > 0) { split(line, f, " "); seconds[f[1]] = f[2] } }
  { name[NR] = $0; cost[NR] = ($0 in seconds) ? seconds[$0] : 5 }
  END {
    # Longest first; equal times keep the order of the names.
    for (a = 1; a <= NR; a++) order[a] = a
    for (a = 2; a <= NR; a++) { k = order[a]; b = a - 1
      while (b >= 1 && cost[order[b]] < cost[k]) { order[b + 1] = order[b]; b-- }
      order[b + 1] = k }
    for (a = 1; a <= NR; a++) { k = order[a]; best = 0
      for (s = 1; s < n; s++) if (load[s] < load[best]) best = s
      load[best] += cost[k]
      if (best == i) print name[k] }
  }')
[ -n "$picked" ] || { echo "slice $index of $total is empty" >&2; exit 1; }
echo "slice $index/$total: $(printf '%s\n' "$picked" | wc -l) of $(printf '%s\n' "$names" | wc -l) tests"
pattern="^($(printf '%s\n' "$picked" | paste -sd '|' -))\$"
go test -race -count=1 -timeout 30m -run "$pattern" "$package"
