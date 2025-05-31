# Base stages for different architectures
FROM --platform=linux/amd64 alpine:3.19.1 AS base-amd64
FROM --platform=linux/arm64 alpine:3.19.1 AS base-arm64

# Architecture-specific cloudflared stage
FROM base-${TARGETARCH} AS cloudflared-base
RUN apk add --no-cache curl

# Install cloudflared based on architecture
RUN ARCH="$(uname -m)"; \
    case "${ARCH}" in \
        x86_64) ARCH_TYPE=amd64 ;; \
        aarch64|arm64) ARCH_TYPE=arm64 ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    curl -Lo /usr/local/bin/cloudflared "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${ARCH_TYPE}" && \
    chmod +x /usr/local/bin/cloudflared

# Architecture-specific inferoute-client stage
FROM base-${TARGETARCH} AS client-base
RUN apk add --no-cache curl unzip

# Add build argument for release version
ARG RELEASE_VERSION=latest

# Download and install inferoute-client binary
RUN ARCH="$(uname -m)"; \
    case "${ARCH}" in \
        x86_64) ARCH_TYPE=amd64 ;; \
        aarch64|arm64) ARCH_TYPE=arm64 ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    BINARY_NAME="inferoute-client-linux-${ARCH_TYPE}" && \
    if [ "$RELEASE_VERSION" = "latest" ]; then \
        DOWNLOAD_URL="https://github.com/inferoute/inferoute-client/releases/latest/download/${BINARY_NAME}.zip"; \
    else \
        DOWNLOAD_URL="https://github.com/inferoute/inferoute-client/releases/download/v${RELEASE_VERSION}/${BINARY_NAME}.zip"; \
    fi && \
    echo "Downloading from: $DOWNLOAD_URL" && \
    curl -Lo "/tmp/${BINARY_NAME}.zip" "$DOWNLOAD_URL" && \
    unzip -o "/tmp/${BINARY_NAME}.zip" -d /tmp && \
    mv "/tmp/${BINARY_NAME}" /usr/local/bin/inferoute-client

# Combine binaries into an intermediate archive stage
FROM scratch AS archive
COPY --from=cloudflared-base /usr/local/bin/cloudflared /bin/cloudflared
COPY --from=client-base /usr/local/bin/inferoute-client /bin/inferoute-client

# Final minimal runtime stage
FROM debian:12-slim

# Install minimal requirements (split into multiple steps for better reliability in emulation)
RUN apt-get update
RUN apt-get install -y --no-install-recommends ca-certificates 
RUN apt-get install -y --no-install-recommends curl bash procps sudo unzip
RUN apt-get clean && rm -rf /var/lib/apt/lists/*

# Copy binaries from archive stage
COPY --from=archive /bin /usr/local/bin
RUN chmod +x /usr/local/bin/cloudflared /usr/local/bin/inferoute-client

# Create required directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

# Copy scripts
COPY scripts/install.sh scripts/entrypoint.sh /app/
RUN chmod +x /app/install.sh /app/entrypoint.sh

# Set working directory
WORKDIR /app

# Expose ports
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
#CMD ["tail", "-f", "/dev/null"]
CMD ["inferoute-client", "--config", "/root/.config/inferoute/config.yaml"] 