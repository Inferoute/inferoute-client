FROM alpine:3.19.1

# Install required packages
RUN apk add --no-cache ca-certificates curl jq bash unzip sudo

# Create config and log directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

# Install NGROK
RUN ARCH="$(uname -m)"; \
    case "${ARCH}" in \
        x86_64) ARCH_TYPE=amd64 ;; \
        aarch64|arm64) ARCH_TYPE=arm64 ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    wget -q https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-${ARCH_TYPE}.tgz -O /tmp/ngrok.tgz && \
    tar xzf /tmp/ngrok.tgz -C /usr/local/bin && \
    rm -f /tmp/ngrok.tgz && \
    chmod +x /usr/local/bin/ngrok

# Set working directory
WORKDIR /app

# Copy scripts
COPY scripts/install.sh scripts/entrypoint.sh /app/
RUN chmod +x /app/install.sh /app/entrypoint.sh

# Expose ports for inferoute-client and NGROK admin interface
EXPOSE 8080 4040

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
CMD ["inferoute-client", "--config", "/root/.config/inferoute/config.yaml"] 