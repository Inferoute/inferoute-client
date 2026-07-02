#!/usr/bin/env bash
# Block until vLLM and inferoute-client are HTTP-ready on JarvisLab.
# Run from Mac after Phases 2–3 (or anytime before inference tests).
#
# Usage:
#   ./wait-for-ready.sh   (config comes from .env next to this script)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/.env}"
if [ -f "$E2E_ENV" ]; then
  # shellcheck source=/dev/null
  source "$E2E_ENV"
fi

# Instance id is discovered from `jl list --json` by name (id rotates on resume).
JL_NAME="${JL_NAME:-Inferoute-client1}"
JL_MACHINE_ID="$(jl list --json 2>/dev/null | jq -r --arg n "$JL_NAME" 'map(select(.name==$n)) | .[0].machine_id // empty')"
[ -n "$JL_MACHINE_ID" ] || { echo "[wait-for-ready] no JarvisLab instance named $JL_NAME (check: jl list)" >&2; exit 1; }

VLLM_WAIT_SEC="${VLLM_WAIT_SEC:-600}"   # model load can take several minutes
CLIENT_WAIT_SEC="${CLIENT_WAIT_SEC:-180}" # client startup

log() { echo "[wait-for-ready] $*"; }

wait_gate() {
  local name="$1" max_sec="$2"
  shift 2
  local start=$SECONDS
  until "$@"; do
    if (( SECONDS - start >= max_sec )); then
      echo "[wait-for-ready] TIMEOUT: $name (>${max_sec}s)" >&2
      echo "Check logs:" >&2
      echo "  jl exec $JL_MACHINE_ID -- tail -80 ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}/vllm.log" >&2
      echo "  jl exec $JL_MACHINE_ID -- tail -80 ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}/inferoute-client.log" >&2
      return 1
    fi
    log "waiting for $name..."
    sleep 15
  done
  log "$name OK"
}

vllm_ready() {
  jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1
}

client_http_ready() {
  jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1
}

log "Gate 1/2: vLLM serving ${VLLM_MODEL:-model} (up to ${VLLM_WAIT_SEC}s)"
wait_gate "vLLM" "$VLLM_WAIT_SEC" vllm_ready

log "Gate 2/2: inferoute-client HTTP (up to ${CLIENT_WAIT_SEC}s)"
wait_gate "inferoute-client" "$CLIENT_WAIT_SEC" client_http_ready

log "Readiness gates passed — safe to run inference tests."
