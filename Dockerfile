FROM ubuntu:22.04

# Prevent apt from prompting for input
ENV DEBIAN_FRONTEND=noninteractive

# Install required packages and ngrok
RUN apt-get update && apt-get install -y \
    curl \
    jq \
    unzip \
    sudo \
    ca-certificates \
    gnupg \
    && curl -fsSL https://ngrok-agent.s3.amazonaws.com/ngrok.asc | dd of=/etc/apt/trusted.gpg.d/ngrok.asc \
    && echo "deb https://ngrok-agent.s3.amazonaws.com buster main" | tee /etc/apt/sources.list.d/ngrok.list \
    && apt-get update \
    && apt-get install -y ngrok \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

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