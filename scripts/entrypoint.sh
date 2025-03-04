#!/bin/bash
set -e

# Ensure backward compatibility - set both environment variables
export LLM_URL="${LLM_URL:-http://localhost:11434}"
export OLLAMA_URL="${OLLAMA_URL:-$LLM_URL}"

# Enable detailed logging for troubleshooting
export LOG_LEVEL=debug
export LOG_FORMAT=console

# Test connection to LLM_URL - particularly important for host.docker.internal
echo "Testing connection to LLM at: $LLM_URL"
if curl -s --max-time 5 --connect-timeout 5 --head --fail "$LLM_URL/v1/models" > /dev/null; then
    echo "Connection to LLM successful"
else
    echo "Warning: Could not connect to LLM at $LLM_URL"
    echo "Detailed connection test with verbose output:"
    curl -v --max-time 5 --connect-timeout 5 "$LLM_URL/v1/models" || true
    echo "Make sure the LLM is running and accessible from the container"
    echo "If using host.docker.internal, ensure you're running in Docker with --add-host=host.docker.internal:host-gateway"
    
    # Check if host.docker.internal resolves
    echo "Checking if host.docker.internal resolves..."
    getent hosts host.docker.internal || echo "host.docker.internal doesn't resolve!"
    
    # Try to ping the host
    echo "Trying to ping host.docker.internal..."
    ping -c 1 host.docker.internal || echo "Cannot ping host.docker.internal"
fi

# Run the install script to download and configure everything
NGROK_AUTHTOKEN="${NGROK_AUTHTOKEN}" PROVIDER_API_KEY="${PROVIDER_API_KEY}" \
PROVIDER_TYPE="${PROVIDER_TYPE:-ollama}" LLM_URL="${LLM_URL}" \
SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh

# Keep container running
exec "$@" 