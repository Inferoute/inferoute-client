FROM golang:1.21-alpine AS builder

# Build arguments for version info
ARG VERSION
ARG COMMIT
ARG DATE

WORKDIR /app

# Copy source code
COPY . .

# Build the binary with version info
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o inferoute-client ./cmd

# Final stage
FROM alpine:3.19.1

# Install required packages
RUN apk add --no-cache ca-certificates curl jq bash unzip sudo

# Create config and log directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

# Set working directory
WORKDIR /app

# Copy install script
COPY scripts/install.sh /app/install.sh
RUN chmod +x /app/install.sh

# Create entrypoint script
RUN echo '#!/bin/bash\n\
set -e\n\
\n\
# Run the install script to download and configure everything\n\
if [ ! -f "/usr/local/bin/inferoute-client" ]; then\n\
  NGROK_AUTHTOKEN="${NGROK_AUTHTOKEN}" PROVIDER_API_KEY="${PROVIDER_API_KEY}" \\\n\
  PROVIDER_TYPE="${PROVIDER_TYPE:-ollama}" OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}" \\\n\
  SERVER_PORT="${SERVER_PORT:-8080}" /app/install.sh\n\
else\n\
  # If binary exists, just configure and start NGROK\n\
  NGROK_AUTHTOKEN="${NGROK_AUTHTOKEN}" ngrok config add-authtoken "${NGROK_AUTHTOKEN}"\n\
  ngrok http ${SERVER_PORT:-8080} --log=stdout --host-header="localhost:${SERVER_PORT:-8080}" > /root/.local/state/inferoute/log/ngrok.log 2>&1 &\n\
fi\n\
\n\
# Keep container running\n\
exec "$@"' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

# Expose ports for inferoute-client and NGROK admin interface
EXPOSE 8080 4040

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
CMD ["inferoute-client", "--config", "/root/.config/inferoute/config.yaml"]

# Copy binary from builder
COPY --from=builder /app/inferoute-client /usr/local/bin/ 