#!/usr/bin/env bash
# Stop JarvisLab provider processes and pause the instance.
# Usage: source ~/.config/inferoute/e2e.env && ./teardown.sh
set -euo pipefail

: "${JL_MACHINE_ID:?set JL_MACHINE_ID in e2e.env}"

echo "=== Teardown: JarvisLab $JL_MACHINE_ID ==="

status=$(jl get "$JL_MACHINE_ID" --json 2>/dev/null | jq -r '.status // empty' || true)
if [ "$status" = "Running" ]; then
  echo "Stopping inferoute-client and vLLM..."
  jl exec "$JL_MACHINE_ID" -- sh -lc \
    'pkill -x inferoute-client 2>/dev/null || true; pkill -f "vllm serve" 2>/dev/null || true' \
    || echo "warn: could not exec stop commands (instance may be unreachable)"
else
  echo "Instance status: ${status:-unknown} — skipping remote process stop"
fi

echo "Pausing instance (stops compute billing)..."
jl pause "$JL_MACHINE_ID" --yes

echo "Done. $JL_MACHINE_ID is paused."
