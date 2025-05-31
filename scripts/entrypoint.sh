#!/bin/bash
set -e

# Run the install script to download and configure everything
# Set default LLM_URL based on provider type
if [ "${PROVIDER_TYPE:-ollama}" = "vllm" ]; then
    DEFAULT_LLM_URL="http://127.0.0.1:8000"
else
    DEFAULT_LLM_URL="http://localhost:11434"
fi

PROVIDER_API_KEY="${PROVIDER_API_KEY}" \
PROVIDER_TYPE="${PROVIDER_TYPE:-ollama}" LLM_URL="${LLM_URL:-$DEFAULT_LLM_URL}" \
SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh

# Keep container running
exec "$@" 