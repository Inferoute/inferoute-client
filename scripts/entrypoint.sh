#!/bin/bash
set -e

# Install or update the client and write its config. A container has no
# terminal for the wizard, so this is always the no-prompt install, which
# needs PROVIDER_API_KEY and PROVIDER_MODEL. install.sh picks the default
# LLM_URL for the engine when it is empty.
INFEROUTE_SKIP_SETUP=1 \
PROVIDER_API_KEY="${PROVIDER_API_KEY:-}" \
PROVIDER_MODEL="${PROVIDER_MODEL:-}" \
PROVIDER_TYPE="${PROVIDER_TYPE:-vllm}" \
LLM_URL="${LLM_URL:-}" \
SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh

# Keep container running
exec "$@"
