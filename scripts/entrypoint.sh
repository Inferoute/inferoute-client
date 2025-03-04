#!/bin/bash
set -e

# Run the install script to download and configure everything
NGROK_AUTHTOKEN="${NGROK_AUTHTOKEN}" PROVIDER_API_KEY="${PROVIDER_API_KEY}" \
PROVIDER_TYPE="${PROVIDER_TYPE:-ollama}" LLM_URL="${LLM_URL:-http://localhost:11434}" \
SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh

# Keep container running
exec "$@" 