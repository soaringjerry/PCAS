#!/usr/bin/env bash
# Own one disposable container; never target a compose project or a name pattern.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "$0")/.." && pwd)"
run_dir="$(mktemp -d /var/tmp/pcas-v2-run.XXXXXXXX)"
container_id=''
local_docker() {
  env -u DOCKER_HOST -u DOCKER_CONTEXT -u DOCKER_TLS -u DOCKER_TLS_VERIFY -u DOCKER_CERT_PATH \
    docker --host=unix:///var/run/docker.sock "$@"
}
cleanup() {
  if [[ -n "$container_id" ]]; then
    owner_label="$(local_docker inspect --format '{{index .Config.Labels "pcas.eval.owner"}}' "$container_id" 2>/dev/null || true)"
    if [[ "$owner_label" == "v2-$$" ]]; then
      local_docker rm -f "$container_id" >/dev/null
    fi
  fi
  if [[ -f "$run_dir/pcas-eval" ]]; then
    rm -- "$run_dir/pcas-eval"
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cd "$repo_dir"
if [[ -n "${PCAS_EVAL_PREPARED_DUMP:-}" ]]; then
  resume_requested=false
  for arg in "$@"; do
    case "$arg" in
      -resume-report|--resume-report|-resume-report=*|--resume-report=*) resume_requested=true ;;
      -private|--private|-private=*|--private=*) echo 'snapshot repair is fictional-only' >&2; exit 1 ;;
    esac
  done
  [[ "$resume_requested" == true && -f "$PCAS_EVAL_PREPARED_DUMP" ]] || {
    echo 'prepared dump requires an explicit fictional resume report' >&2
    exit 1
  }
fi
go build -o "$run_dir/pcas-eval" ./cmd/pcas-eval
container_id="$(local_docker run -d --rm \
  --label "pcas.eval.owner=v2-$$" \
  --name "pcas-v2-$(date -u +%Y%m%dT%H%M%S)-$$" \
  -e POSTGRES_USER=pcas_v2 -e POSTGRES_PASSWORD=v2-disposable-only -e POSTGRES_DB=pcas_v2 \
  --tmpfs /var/lib/postgresql/data:rw --shm-size=64m \
  -p 127.0.0.1::5432 pgvector/pgvector:0.8.2-pg16-bookworm)"
ready=false
for attempt in {1..60}; do
  if local_docker exec "$container_id" pg_isready -h 127.0.0.1 -U pcas_v2 -d pcas_v2 >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
[[ "$ready" == true ]] || { echo 'disposable database failed to start' >&2; exit 1; }
if [[ -n "${PCAS_EVAL_PREPARED_DUMP:-}" ]]; then
  # Restore only into the new container this script just created. Never point
  # pg_restore at a supplied DSN or an existing server/container.
  local_docker exec -i "$container_id" pg_restore --exit-on-error --no-owner --no-privileges \
    -U pcas_v2 -d pcas_v2 < "$PCAS_EVAL_PREPARED_DUMP"
  sha256sum -- "$PCAS_EVAL_PREPARED_DUMP"
fi
port="$(local_docker port "$container_id" 5432/tcp)"
port="${port##*:}"
revision="$(git rev-parse HEAD)"
"$run_dir/pcas-eval" -mode=doing \
  -database-url "postgres://pcas_v2:v2-disposable-only@127.0.0.1:$port/pcas_v2?sslmode=disable" \
  -revision "$revision" -output "$run_dir/report" "$@"
printf 'metrics-only report: %s/report.{json,md}\n' "$run_dir"
