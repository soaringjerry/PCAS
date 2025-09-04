#!/usr/bin/env bash
set -euo pipefail

# PCAS install-or-update script (Docker-based)
# Usage examples:
#   bash -c "$(curl -fsSL https://raw.githubusercontent.com/soaringjerry/pcas/main/scripts/install-or-update.sh)" -- \
#     --dir /opt/pcas --name pcas-instance --port 50051
#
#   bash -c "$(curl -fsSL https://raw.githubusercontent.com/soaringjerry/pcas/main/scripts/install-or-update.sh)" -- \
#     --dir /opt/pcas --openai-key "sk-..." --policy-url https://raw.githubusercontent.com/soaringjerry/pcas/main/policy.yaml

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

DIR="/opt/pcas"
NAME="pcas-instance"
PORT="50051"
IMAGE="ghcr.io/soaringjerry/pcas:latest"
VOLUME="pcas_data"
POLICY_PATH=""
POLICY_URL=""
OPENAI_KEY="${OPENAI_API_KEY:-}"
ADMIN_TOKEN=""
PULL_IMAGE=1
START_CONTAINER=1

log() { echo -e "${GREEN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}WARN:${NC} $*"; }
err() { echo -e "${RED}ERROR:${NC} $*" 1>&2; }

usage() {
  cat <<EOF
PCAS install-or-update (Docker)

Options:
  --dir PATH           Install directory (default: /opt/pcas)
  --name NAME          Docker container name (default: pcas-instance)
  --port PORT          Host port for gRPC (default: 50051)
  --image IMAGE        Docker image (default: ghcr.io/soaringjerry/pcas:latest)
  --volume NAME        Docker volume for data (default: pcas_data)
  --policy PATH        Local policy.yaml to mount
  --policy-url URL     Download policy.yaml from URL to --dir/policy.yaml
  --openai-key KEY     Provide OpenAI API key (or set OPENAI_API_KEY env)
  --no-pull            Do not pull image (use local cache)
  --no-start           Do not start/restart container (prepare files only)
  -h, --help           Show this help

Examples:
  bash -c "$(curl -fsSL https://raw.githubusercontent.com/soaringjerry/pcas/main/scripts/install-or-update.sh)" -- \
    --dir /opt/pcas --name pcas-instance --port 50051

  OPENAI_API_KEY=sk-... bash -c "$(curl -fsSL https://raw.githubusercontent.com/soaringjerry/pcas/main/scripts/install-or-update.sh)" -- \
    --dir /opt/pcas --policy-url https://raw.githubusercontent.com/soaringjerry/pcas/main/policy.yaml
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) DIR="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --image) IMAGE="$2"; shift 2 ;;
    --volume) VOLUME="$2"; shift 2 ;;
    --policy) POLICY_PATH="$2"; shift 2 ;;
    --policy-url) POLICY_URL="$2"; shift 2 ;;
    --openai-key) OPENAI_KEY="$2"; shift 2 ;;
    --admin-token) ADMIN_TOKEN="$2"; shift 2 ;;
    --no-pull) PULL_IMAGE=0; shift ;;
    --no-start) START_CONTAINER=0; shift ;;
    --pcas) _pcas_host="$2"; shift 2 ;; # accepted for compatibility, unused
    -h|--help) usage; exit 0 ;;
    *) err "Unknown option: $1"; usage; exit 1 ;;
  esac
done

# Check docker
if ! command -v docker >/dev/null 2>&1; then
  err "Docker is required but not found. Please install Docker and re-run."
  exit 1
fi

# Prepare directory
log "Preparing install dir: ${DIR}"
mkdir -p "${DIR}"

# Prepare policy
TARGET_POLICY="${DIR}/policy.yaml"
if [[ -n "${POLICY_PATH}" ]]; then
  if [[ ! -f "${POLICY_PATH}" ]]; then
    err "--policy path not found: ${POLICY_PATH}"
    exit 1
  fi
  cp -f "${POLICY_PATH}" "${TARGET_POLICY}"
  log "Copied policy.yaml from ${POLICY_PATH}"
elif [[ -n "${POLICY_URL}" ]]; then
  log "Downloading policy.yaml from ${POLICY_URL}"
  curl -fsSL "${POLICY_URL}" -o "${TARGET_POLICY}"
elif [[ ! -f "${TARGET_POLICY}" ]]; then
  log "No policy.yaml provided; downloading default from repository"
  DEFAULT_URL="https://raw.githubusercontent.com/soaringjerry/pcas/main/policy.yaml"
  if curl -fsSL "${DEFAULT_URL}" -o "${TARGET_POLICY}"; then
    :
  else
    warn "Failed to download default policy; writing a minimal template"
    cat > "${TARGET_POLICY}" <<'POLICY'
version: v1
providers:
  - name: mock-provider
    type: mock
rules:
  - name: "echo"
    if:
      event_type: "pcas.echo.v1"
    then:
      provider: mock-provider
POLICY
  fi
fi

# Ensure data volume
log "Ensuring data volume: ${VOLUME}"
docker volume create "${VOLUME}" >/dev/null

# Optionally pull image
if [[ "${PULL_IMAGE}" -eq 1 ]]; then
  log "Pulling image: ${IMAGE}"
  docker pull "${IMAGE}"
else
  warn "Skipping docker pull (using local image cache)"
fi

if [[ "${START_CONTAINER}" -eq 0 ]]; then
  log "Preparation completed (no-start mode)."
  exit 0
fi

# Stop/remove existing container
log "Stopping existing container if present"
docker stop "${NAME}" >/dev/null 2>&1 || true
docker rm "${NAME}" >/dev/null 2>&1 || true

# Run container
RUN_ARGS=(run -d --name "${NAME}" --restart unless-stopped \
  -p "${PORT}:50051" \
  -v "${TARGET_POLICY}:/app/policy.yaml:ro" \
  -v "${VOLUME}:/data")

if [[ -n "${OPENAI_KEY}" ]]; then
  RUN_ARGS+=(-e "OPENAI_API_KEY=${OPENAI_KEY}")
else
  warn "OPENAI_API_KEY not provided; search/RAG will be disabled (server still runs)"
fi

if [[ -n "${ADMIN_TOKEN}" ]]; then
  RUN_ARGS+=(-e "PCAS_ADMIN_TOKEN=${ADMIN_TOKEN}")
fi

RUN_ARGS+=("${IMAGE}" \
  /app/bin/pcas serve --db-path /data/pcas.db)

log "Starting container: ${NAME} (port ${PORT})"
docker "${RUN_ARGS[@]}"

# Simple readiness check
log "Waiting for gRPC port to be available (localhost:${PORT})"
ATTEMPTS=30
for i in $(seq 1 ${ATTEMPTS}); do
  if command -v nc >/dev/null 2>&1; then
    if nc -z 127.0.0.1 "${PORT}" >/dev/null 2>&1; then
      log "PCAS gRPC port is open."
      break
    fi
  else
    # Fallback: try docker logs grep
    if docker logs "${NAME}" 2>&1 | grep -q "PCAS server starting on"; then
      log "PCAS reported start in logs."
      break
    fi
  fi
  sleep 2
  echo -n "."
done
echo

log "Done. View logs with: docker logs -f ${NAME}"
echo "Try: ./bin/pcasctl emit --server 127.0.0.1:${PORT} --type pcas.echo.v1 --data '{\"message\":\"hello\"}'"
