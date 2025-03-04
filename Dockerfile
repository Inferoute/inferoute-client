# Base stages for different architectures
FROM --platform=linux/amd64 alpine:3.19.1 AS base-amd64
FROM --platform=linux/arm64 alpine:3.19.1 AS base-arm64

# Architecture-specific ngrok stage
FROM base-${TARGETARCH} AS ngrok-base
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
    rm -f /tmp/ngrok.tgz

# Architecture-specific inferoute-client stage
FROM base-${TARGETARCH} AS client-base
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
    mv "/tmp/${BINARY_NAME}" /usr/local/bin/inferoute-client

# Combine binaries into an intermediate archive stage
FROM scratch AS archive
COPY --from=ngrok-base /usr/local/bin/ngrok /bin/ngrok
COPY --from=client-base /usr/local/bin/inferoute-client /bin/inferoute-client

# Final minimal runtime stage
FROM ubuntu:22.04
RUN apt-get update \
    && apt-get install -y ca-certificates curl jq bash \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries from archive stage
COPY --from=archive /bin /usr/local/bin
RUN chmod +x /usr/local/bin/ngrok /usr/local/bin/inferoute-client

# Create required directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

# Copy scripts
COPY scripts/install.sh scripts/entrypoint.sh /app/
RUN chmod +x /app/install.sh /app/entrypoint.sh

# Set working directory
WORKDIR /app

# Expose ports
EXPOSE 8080 4040

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
CMD ["inferoute-client", "--config", "/root/.config/inferoute/config.yaml"] 