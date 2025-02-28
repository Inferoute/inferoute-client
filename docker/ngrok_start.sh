#!/bin/bash
set -e

# Start NGROK HTTP tunnel
echo "Starting NGROK tunnel..."
/usr/local/bin/ngrok http ${SERVER_PORT:-8080} --log=stdout > /dev/null &

# Wait for NGROK to start
echo "Waiting for NGROK to start..."
sleep 5

# Get NGROK public URL
echo "Getting NGROK public URL..."
NGROK_PUBLIC_URL=""
while [ -z "$NGROK_PUBLIC_URL" ]; do
    echo "Trying to get NGROK public URL..."
    NGROK_PUBLIC_URL=$(curl -s http://localhost:4040/api/tunnels | jq -r '.tunnels[0].public_url')
    
    if [ "$NGROK_PUBLIC_URL" == "null" ] || [ -z "$NGROK_PUBLIC_URL" ]; then
        echo "NGROK not ready yet, waiting..."
        NGROK_PUBLIC_URL=""
        sleep 2
    fi
done

echo "NGROK public URL: $NGROK_PUBLIC_URL"

# Update config.yaml with NGROK URL
sed -i "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_PUBLIC_URL\"|" /app/config.yaml

# Keep the script running
tail -f /dev/null 