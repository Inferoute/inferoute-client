#!/bin/bash
set -e

# Run the install script to download and configure everything
DEFAULT_LLM_URL="http://127.0.0.1:8000"

PROVIDER_API_KEY="${PROVIDER_API_KEY}" \
PROVIDER_TYPE="${PROVIDER_TYPE:-vllm}" LLM_URL="${LLM_URL:-$DEFAULT_LLM_URL}" \
SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh

# Keep container running
exec "$@" 