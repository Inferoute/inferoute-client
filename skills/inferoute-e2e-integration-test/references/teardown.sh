#!/usr/bin/env bash
# Pause the JarvisLab instance (stops compute billing).
# Pausing halts vLLM + inferoute-client too — no need to stop them first.
# Usage: source ~/.config/inferoute/e2e.env && ./teardown.sh
set -euo pipefail

: "${JL_MACHINE_ID:?set JL_MACHINE_ID in e2e.env}"

echo "=== Teardown: pausing JarvisLab $JL_MACHINE_ID ==="
jl pause "$JL_MACHINE_ID" --yes
echo "Done. $JL_MACHINE_ID is paused."
