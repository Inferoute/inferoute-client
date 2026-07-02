#!/usr/bin/env bash
# Pause the JarvisLab instance (stops compute billing).
# Pausing halts vLLM + inferoute-client too — no need to stop them first.
# Usage: ./teardown.sh   (config comes from .env next to this script)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/.env}"
# shellcheck source=/dev/null
[ -f "$E2E_ENV" ] && source "$E2E_ENV"

# Instance id is discovered from `jl list --json` by name (id rotates on resume).
JL_NAME="${JL_NAME:-Inferoute-client1}"
JL_MACHINE_ID="$(jl list --json 2>/dev/null | jq -r --arg n "$JL_NAME" 'map(select(.name==$n)) | .[0].machine_id // empty')"
[ -n "$JL_MACHINE_ID" ] || { echo "no JarvisLab instance named $JL_NAME (check: jl list)" >&2; exit 1; }

echo "=== Teardown: pausing JarvisLab $JL_MACHINE_ID ==="
jl pause "$JL_MACHINE_ID" --yes
echo "Done. $JL_MACHINE_ID is paused."
