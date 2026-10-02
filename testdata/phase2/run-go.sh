#!/usr/bin/env bash
# One run, one tmpfs PostgreSQL; no host configuration or live credentials.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
run_dir=$(mktemp -d /tmp/pcas-b1-T-go-XXXXXX)
container_id=''
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [[ -n "$container_id" ]]; then docker rm -fv "$container_id" >/dev/null 2>&1 || true; fi
  echo "Acceptance artifacts: $run_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
container_id=$(docker run -d --tmpfs /var/lib/postgresql/data:rw,size=1g \
  -e POSTGRES_USER=b1_test -e POSTGRES_PASSWORD=b1-synthetic-only -e POSTGRES_DB=b1_acceptance \
  -p 127.0.0.1::5432 pgvector/pgvector:0.8.2-pg16-bookworm)
printf '%s\n' "$container_id" > "$run_dir/container.id"
for i in {1..30}; do
  if docker exec "$container_id" pg_isready -U b1_test -d b1_acceptance >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$container_id" pg_isready -U b1_test -d b1_acceptance >/dev/null
port=$(docker port "$container_id" 5432)
export PCAS_TEST_DATABASE_URL="postgres://b1_test:b1-synthetic-only@$port/b1_acceptance?sslmode=disable"
unset PCAS_DATABASE_URL PCAS_MODELS_FILE PCAS_MODEL_SETTINGS_FILE PCAS_NOTIFY_SETTINGS_FILE
unset PCAS_CODEX_BINARY PCAS_CODEX_HOME PCAS_AGENT_TOKENS_FILE OPENAI_API_KEY ANTHROPIC_API_KEY
unset PCAS_TEST_CODEX_BINARY PCAS_LIVE_CODEX_BINARY PCAS_LIVE_CODEX_HOME PCAS_LIVE_EMBEDDING_URL
if (($#)); then
  "$@"
else
  go test -json ./internal/postgres -run '^TestPhase2B1_' -count=1 -timeout=12m | tee "$run_dir/go.jsonl"
fi
