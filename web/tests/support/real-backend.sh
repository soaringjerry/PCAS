#!/usr/bin/env bash
# Disposable real PCAS API + worker; only external providers are httptest fakes.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
root=$PWD
# Validate before allocating resources, including for custom-command runs.
round=${PCAS_REAL_BACKEND_ROUND:-}
case "$round" in
  '') golden_repeats=3; run_auxiliary=true ;;
  1) golden_repeats=1; run_auxiliary=true ;;
  2|3) golden_repeats=1; run_auxiliary=false ;;
  *) echo "PCAS_REAL_BACKEND_ROUND must be 1, 2, or 3 (or unset)" >&2; exit 2 ;;
esac
mkdir -p "$root/data"
run_dir=$(mktemp -d "$root/data/golden-XXXXXX")
container="pcas-test-E-$$"
container_id=""
pids=()
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${pids[@]}"; do wait "$pid" 2>/dev/null || true; done
  if [[ -n "$container_id" ]]; then docker rm -fv "$container_id" >/dev/null 2>&1 || true; fi
  mkdir -p "$root/web/test-results/services"
  cp "$run_dir"/*.log "$root/web/test-results/services/" 2>/dev/null || true
  rm -rf "$run_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# No credentials, local notification state, or account from the host is reused.
unset OPENAI_API_KEY ANTHROPIC_API_KEY PCAS_MODEL_API_KEY TYPESAFE_API_KEY
unset PCAS_CODEX_BINARY PCAS_CODEX_HOME PCAS_AGENT_TOKENS_FILE PCAS_INBOX_DIR
export PCAS_CHATGPT_DIRECT_ENABLED=${PCAS_CHATGPT_DIRECT_ENABLED:-true}
export PCAS_CHATGPT_DIR="$run_dir/chatgpt"
export PCAS_CHATGPT_CALLBACK_PORT=14559
export PCAS_MODEL_SETTINGS_FILE="$run_dir/settings.json"
export PCAS_NOTIFY_SETTINGS_FILE="$run_dir/notify.json"
export PCAS_BLOB_DIR="$run_dir/blobs"
export PCAS_WEB_DIR="$root/web/dist"
export PCAS_OWNER_ID=$(cat /proc/sys/kernel/random/uuid)
export PCAS_API_TOKEN="pcas-golden-test-only-$PCAS_OWNER_ID"
export PCAS_TEST_API_TOKEN="$PCAS_API_TOKEN"
export PCAS_HTTP_ADDR="127.0.0.1:${PCAS_GOLDEN_PORT:-18097}"
export PCAS_TEST_BASE_URL="http://$PCAS_HTTP_ADDR"
export PCAS_PUBLIC_URL="$PCAS_TEST_BASE_URL"

container_id=$(docker run -d --name "$container" --tmpfs /var/lib/postgresql/data:rw,size=1g \
  -e POSTGRES_PASSWORD=test -p 127.0.0.1::5432 pgvector/pgvector:0.8.2-pg16-bookworm)
printf '%s\n' "$container_id" > "$run_dir/container.id"
for i in {1..30}; do
  if docker exec "$container" pg_isready -U postgres >/dev/null 2>&1; then break; fi
  sleep 1
done
port=$(docker port "$container" 5432)
export PCAS_DATABASE_URL="postgres://postgres:test@${port}/postgres?sslmode=disable"
export PCAS_TEST_DATABASE_URL="$PCAS_DATABASE_URL"
go build -o "$run_dir/fixture" ./internal/testsupport/golden
go build -o "$run_dir/pcas" ./cmd/pcas
"$run_dir/fixture" "$run_dir" > "$run_dir/fixture.log" 2>&1 &
pids+=("$!")
for i in {1..50}; do test -f "$run_dir/fixture.env" && break; sleep .1; done
source "$run_dir/fixture.env"
# Capture complete model HTTP bodies for independent batch1 browser acceptance.
go build -o "$run_dir/b1-capture" ./web/tests/support/phase2-capture
"$run_dir/b1-capture" "$PCAS_MODELS_FILE" "$run_dir/b1-capture.env" > "$run_dir/b1-capture.log" 2>&1 &
pids+=("$!")
for i in {1..50}; do test -f "$run_dir/b1-capture.env" && break; sleep .1; done
source "$run_dir/b1-capture.env"
"$run_dir/pcas" migrate > "$run_dir/migrate.log" 2>&1
"$run_dir/pcas" serve > "$run_dir/serve.log" 2>&1 &
pids+=("$!")
"$run_dir/pcas" worker > "$run_dir/worker.log" 2>&1 &
pids+=("$!")
for i in {1..30}; do
  if curl -fsS "$PCAS_TEST_BASE_URL/readyz" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "$PCAS_TEST_BASE_URL/readyz" >/dev/null
cd web
if (($#)); then
  "$@"
else
  status=0
  # Round 1 owns auxiliary coverage; no selector retains the full local suite.
  if "$run_auxiliary"; then
    # The first-login default requires the runner's fresh workspace.
    PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/timezone.json npx playwright test tests/timezone-backend.spec.ts \
      --output=test-results/timezone --reporter=list,json || status=1
  fi
  PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/golden.json npx playwright test tests/golden.spec.ts \
    --repeat-each="$golden_repeats" --output=test-results/golden --reporter=list,json || status=1
  if "$run_auxiliary"; then
    PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/legacy.json npx playwright test tests/backend.spec.ts \
      tests/continuity.spec.ts tests/chatgpt-direct.spec.ts tests/model-api.spec.ts tests/phase2-batch1-backend.spec.ts \
      --output=test-results/legacy --reporter=list,json || status=1
  fi
  exit "$status"
fi
