#!/bin/bash
set -e

# Check if config.yaml exists, if not copy from example
if [ ! -f /app/config.yaml ]; then
    echo "ERROR: config.yaml not found. Please create a config.yaml file before running the container."
    echo "You can use config.yaml.example as a template."
    exit 1
fi

# Check if NGROK authtoken is in config.yaml
NGROK_AUTHTOKEN=$(grep -A 5 "ngrok:" /app/config.yaml | grep "authtoken:" | awk -F'"' '{print $2}')
if [ -z "$NGROK_AUTHTOKEN" ]; then
    echo "ERROR: NGROK authtoken not found in config.yaml"
    echo "Please add 'authtoken: \"your_ngrok_authtoken_here\"' under the ngrok section in config.yaml"
    exit 1
fi

# Configure NGROK
echo "Configuring NGROK..."
mkdir -p /root/.ngrok2
cat > /root/.ngrok2/ngrok.yml << EOF
authtoken: $NGROK_AUTHTOKEN
version: 2
region: us
EOF

# Get server port from config.yaml
SERVER_PORT=$(grep -A 5 "server:" /app/config.yaml | grep "port:" | awk '{print $2}')
if [ -z "$SERVER_PORT" ]; then
    SERVER_PORT=8080
    echo "Server port not found in config.yaml, using default: $SERVER_PORT"
fi

# Export server port for use in ngrok_start.sh
export SERVER_PORT

# Create docker directory if it doesn't exist
mkdir -p /app/docker

# Make sure script files are executable
chmod +x /app/docker/ngrok_start.sh
chmod +x /app/docker/inferoute_start.sh

# Start supervisord to manage processes
echo "Starting supervisord..."
exec /usr/bin/supervisord -c /etc/supervisord.conf 