FROM alpine:3.19.1

# Install required packages
RUN apk add --no-cache ca-certificates curl jq bash unzip sudo

# Create config and log directories
RUN mkdir -p /root/.config/inferoute /root/.local/state/inferoute/log

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