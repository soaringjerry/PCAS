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

ORIG_ARGS=("$@")

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
NO_PROMPT=0
ASSUME_YES=0
RESET_CONFIG=0

CFG_FILE=""

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
  --admin-token KEY    Set admin token for dynamic policy updates (PCAS_ADMIN_TOKEN)
  --no-pull            Do not pull image (use local cache)
  --no-start           Do not start/restart container (prepare files only)
  --no-prompt          Unattended mode; reuse saved config or provided flags
  -y, --yes            Assume Yes for prompts (where applicable)
  --reset-config       Ignore saved config and re-run guided setup
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
    --no-prompt) NO_PROMPT=1; shift ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    --reset-config) RESET_CONFIG=1; shift ;;
    --pcas) _pcas_host="$2"; shift 2 ;; # accepted for compatibility, unused
    -h|--help) usage; exit 0 ;;
    *) err "Unknown option: $1"; usage; exit 1 ;;
  esac
done

# Config helpers
is_tty() { [[ -t 0 ]] || [[ -t 1 ]]; }

ask() {
  local prompt="$1"; local def="${2:-}"; local var
  if [[ -n "$def" ]]; then
    read -r -p "$prompt [$def]: " var || true
    echo "${var:-$def}"
  else
    read -r -p "$prompt: " var || true
    echo "$var"
  fi
}

ask_secret() {
  local prompt="$1"; local def_masked="${2:-}"; local var
  read -r -s -p "$prompt: " var || true; echo
  echo "$var"
}

confirm() {
  local prompt="$1"; local def_yes=${2:-1}; local ans
  local def_text="Y/n"; [[ $def_yes -eq 0 ]] && def_text="y/N"
  if [[ $ASSUME_YES -eq 1 ]]; then return 0; fi
  read -r -p "$prompt [$def_text]: " ans || true
  ans="${ans:-}"
  if [[ -z "$ans" ]]; then [[ $def_yes -eq 1 ]] && return 0 || return 1; fi
  [[ "$ans" =~ ^[Yy]$ ]] && return 0 || return 1
}

CFG_FILE="${DIR}/pcas-installer.env"

save_cfg() {
  : >"$CFG_FILE"
  {
    printf 'DIR=%q\n' "$DIR"
    printf 'NAME=%q\n' "$NAME"
    printf 'PORT=%q\n' "$PORT"
    printf 'IMAGE=%q\n' "$IMAGE"
    printf 'VOLUME=%q\n' "$VOLUME"
    printf 'OPENAI_KEY=%q\n' "$OPENAI_KEY"
    printf 'ADMIN_TOKEN=%q\n' "$ADMIN_TOKEN"
  } >>"$CFG_FILE"
  chmod 600 "$CFG_FILE"
}

if [[ -f "$CFG_FILE" && $RESET_CONFIG -eq 0 ]]; then
  # Try to load saved config; if it fails, fall back to guided setup
  set +e
  # shellcheck disable=SC1090
  source "$CFG_FILE"
  load_rc=$?
  set -e
  if [[ $load_rc -ne 0 ]]; then
    warn "Failed to load saved config ($CFG_FILE); switching to guided setup"
    RESET_CONFIG=1
  fi
fi

# Guided setup (first run or reset), only when interactive
if is_tty && [[ $NO_PROMPT -eq 0 ]]; then
  if [[ ! -f "$CFG_FILE" || $RESET_CONFIG -eq 1 ]]; then
    echo ""
    echo "PCAS guided setup (first-time configuration)"
    NAME=$(ask "Container name" "${NAME}")
    PORT=$(ask "gRPC port" "${PORT}")
    if confirm "Provide OpenAI API key now?" 0; then
      OPENAI_KEY=$(ask_secret "Enter OPENAI_API_KEY")
    fi
    if confirm "Set admin token (enables secure dynamic policy updates)?" 0; then
      ADMIN_TOKEN=$(ask_secret "Enter PCAS_ADMIN_TOKEN")
    fi
    mkdir -p "${DIR}" && chmod 755 "${DIR}"
    save_cfg
    log "Saved installer config to $CFG_FILE"
  else
    echo ""
    echo "Using saved config from $CFG_FILE"
    echo "  DIR=${DIR}"
    echo "  NAME=${NAME}  PORT=${PORT}  IMAGE=${IMAGE}  VOLUME=${VOLUME}"
    if confirm "Use existing config as-is?" 1; then
      :
    else
      NAME=$(ask "Container name" "${NAME}")
      PORT=$(ask "gRPC port" "${PORT}")
      if confirm "Update OpenAI API key?" 0; then
        OPENAI_KEY=$(ask_secret "Enter OPENAI_API_KEY (leave blank to remove)")
      fi
      if confirm "Update admin token?" 0; then
        ADMIN_TOKEN=$(ask_secret "Enter PCAS_ADMIN_TOKEN (leave blank to remove)")
      fi
      save_cfg
      log "Updated installer config"
    fi
  fi
fi

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
