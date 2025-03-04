# Base stage for installing ngrok
FROM alpine:3.19.1 AS ngrok-base

# Install minimal requirements for ngrok
RUN apk add --no-cache curl

# Install NGROK based on architecture
RUN ARCH="$(uname -m)"; \
    case "${ARCH}" in \
        x86_64) ARCH_TYPE=amd64 ;; \
        aarch64|arm64) ARCH_TYPE=arm64 ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    curl -Lo /tmp/ngrok.tgz "https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-${ARCH_TYPE}.tgz" && \
    tar xzf /tmp/ngrok.tgz -C /usr/local/bin && \
    rm -f /tmp/ngrok.tgz && \
    chmod +x /usr/local/bin/ngrok

# Stage for downloading inferoute-client
FROM alpine:3.19.1 AS client-base

# Install minimal requirements
RUN apk add --no-cache curl unzip

# Download and install inferoute-client binary
RUN ARCH="$(uname -m)"; \
    case "${ARCH}" in \
        x86_64) ARCH_TYPE=amd64 ;; \
        aarch64|arm64) ARCH_TYPE=arm64 ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    BINARY_NAME="inferoute-client-linux-${ARCH_TYPE}" && \
    curl -Lo "/tmp/${BINARY_NAME}.zip" "https://github.com/inferoute/inferoute-client/releases/latest/download/${BINARY_NAME}.zip" && \
    unzip -o "/tmp/${BINARY_NAME}.zip" -d /tmp && \
    mv "/tmp/${BINARY_NAME}" /usr/local/bin/inferoute-client && \
    chmod +x /usr/local/bin/inferoute-client && \
    rm -f "/tmp/${BINARY_NAME}.zip"

# Final stage
FROM ubuntu:22.04

# Install minimal requirements
RUN apt-get update && \
    apt-get install -y ca-certificates jq && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create config and log directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

# Copy binaries from previous stages
COPY --from=ngrok-base /usr/local/bin/ngrok /usr/local/bin/
COPY --from=client-base /usr/local/bin/inferoute-client /usr/local/bin/

# Copy scripts and config template
COPY scripts/entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh

# Set working directory
WORKDIR /app

# Expose ports
EXPOSE 8080 4040

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
CMD ["inferoute-client", "--config", "/root/.config/inferoute/config.yaml"] 