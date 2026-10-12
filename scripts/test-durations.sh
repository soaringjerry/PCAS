#!/usr/bin/env bash
# Writes scripts/test-durations.txt from the logs of the twelve PostgreSQL
# slices of one pull request's CI run. The slices run with -v, so each log
# has one "--- PASS: <test> (<seconds>s)" line for each top-level test.
#   scripts/test-durations.sh <pull request number>
set -euo pipefail
pr=${1:?pull request number}
repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
out=$(dirname "$0")/test-durations.txt
gh pr checks "$pr" | awk -F'\t' '$1 ~ /^postgres \(/ && !seen[$1]++ {print $4}' | sed -E 's#.*/job/([0-9]+).*#\1#' | while read -r job; do
  gh api "repos/$repo/actions/jobs/$job/logs"
done | sed -nE 's/^[^ ]+ --- (PASS|FAIL): (Test[^ \/]+) \(([0-9.]+)s\)$/\2 \3/p' |
  awk '{ s = int($2 + 0.5); if (s < 1) s = 1; print $1, s }' | sort -u -k1,1 > "$out"
echo "$(wc -l < "$out") tests in $out"
