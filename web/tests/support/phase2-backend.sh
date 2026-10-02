#!/usr/bin/env bash
# Random local API port; the shared runner records only its own processes and
# container. The first-party test capture is a local fake model, not a provider.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
export PCAS_GOLDEN_PORT=$(python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(('127.0.0.1',0))
    print(s.getsockname()[1])
PY
)
# Direct subscription is not needed for batch1 and should own no callback port.
export PCAS_CHATGPT_DIRECT_ENABLED=false
bash web/tests/support/real-backend.sh npx playwright test tests/phase2-batch1.spec.ts tests/phase2-batch1-backend.spec.ts --retries=0
