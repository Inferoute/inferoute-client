#!/bin/bash
set -e

# Wait for NGROK to be ready
echo "Waiting for NGROK to be ready..."
while [ ! -f /var/log/ngrok.log ] || ! grep -q "started tunnel" /var/log/ngrok.log; do
    echo "NGROK not ready yet, waiting..."
    sleep 2
done

echo "NGROK is ready, starting inferoute-client..."

# Start inferoute-client
cd /app
exec ./inferoute-client -config /app/config.yaml 