#!/usr/bin/env bash
# Run ON the JarvisLab instance (via jl exec 'sh -lc ...' or jl ssh).
# Idempotent start of the PRE-INSTALLED vLLM + inferoute-client.
#
# The instance ships with both already built (persistent across resume):
#   vLLM binary : /home/ubuntu/vllm-env/bin/vllm
#   client dir  : /home/ubuntu/inferoute-client  (./inferoute-client + config.yaml)
# Do NOT pip install or run the install script — the binaries persist.
set -euo pipefail

: "${VLLM_MODEL:?}"

VLLM_BIN="${JL_VLLM_BIN:-/home/ubuntu/vllm-env/bin/vllm}"
CLIENT_DIR="${JL_CLIENT_DIR:-/home/ubuntu/inferoute-client}"
LOG_DIR="${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}"

mkdir -p "$LOG_DIR"

# --- vLLM -------------------------------------------------------------------
# Detect by PORT, not pgrep: `pgrep -f "vllm serve"` self-matches the shell
# command string when invoked through `jl exec sh -lc '...'`.
# Launch with setsid + </dev/null so it survives the exec/ssh session.
if curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; then
  echo "vLLM already serving"
else
  echo "Launching vLLM ($VLLM_MODEL)..."
  setsid "$VLLM_BIN" serve "$VLLM_MODEL" --host 0.0.0.0 --port 8000 \
    >"$LOG_DIR/vllm.log" 2>&1 </dev/null &
fi

for _ in $(seq 1 40); do
  curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1 && break
  sleep 15
done
curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1 || { echo "vLLM not ready"; tail -30 "$LOG_DIR/vllm.log"; exit 1; }

# --- inferoute-client -------------------------------------------------------
# Uses the persistent config at $CLIENT_DIR/config.yaml (url + api_key already set).
if curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1; then
  echo "inferoute-client already running"
else
  echo "Launching inferoute-client..."
  setsid "$CLIENT_DIR/inferoute-client" --config "$CLIENT_DIR/config.yaml" \
    >"$LOG_DIR/inferoute-client.log" 2>&1 </dev/null &
fi

for _ in $(seq 1 12); do
  curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1 && break
  sleep 10
done
curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1 || { echo "inferoute-client not ready"; tail -30 "$LOG_DIR/inferoute-client.log"; exit 1; }

echo "vLLM models:" && curl -s http://127.0.0.1:8000/v1/models | head -c 400
echo ""
echo "Client health:" && curl -s http://127.0.0.1:8080/api/health | head -c 600
