#!/usr/bin/env bash
# Run ON the JarvisLab instance (via jl ssh or upload + exec).
# Idempotent bootstrap for vLLM + inferoute-client.
set -euo pipefail

: "${PROVIDER_API_KEY:?}"
: "${INFEROUTE_PLATFORM_URL:?}"
: "${VLLM_MODEL:?}"

LOG_DIR=/home/inferoute/logs
mkdir -p "$LOG_DIR" /home/inferoute/bin

if ! command -v vllm >/dev/null 2>&1; then
  pip install -q 'vllm>=0.8.0'
fi

if ! pgrep -f "vllm serve" >/dev/null; then
  nohup vllm serve "$VLLM_MODEL" --host 0.0.0.0 --port 8000 \
    >"$LOG_DIR/vllm.log" 2>&1 &
fi

for i in $(seq 1 40); do
  curl -sf http://127.0.0.1:8000/v1/models >/dev/null && break
  sleep 15
done

if ! command -v inferoute-client >/dev/null 2>&1; then
  export PROVIDER_TYPE=vllm LLM_URL=http://127.0.0.1:8000 SERVER_PORT=8080
  curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | bash
fi

mkdir -p ~/.config/inferoute
cat >~/.config/inferoute/config.yaml <<EOF
server:
  port: 8080
  host: "0.0.0.0"
provider:
  api_key: "$PROVIDER_API_KEY"
  url: "$INFEROUTE_PLATFORM_URL"
  provider_type: vllm
  llm_url: http://127.0.0.1:8000
logging:
  level: info
EOF

if ! pgrep -x inferoute-client >/dev/null; then
  nohup inferoute-client --config ~/.config/inferoute/config.yaml \
    >"$LOG_DIR/inferoute-client.log" 2>&1 &
fi

echo "vLLM models:" && curl -s http://127.0.0.1:8000/v1/models | head -c 400
echo ""
echo "Client health:" && curl -s http://127.0.0.1:8080/api/health | head -c 600
