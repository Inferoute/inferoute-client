#!/bin/bash
set -e

# Check if config.yaml already exists
if [ -f "config.yaml" ]; then
    read -p "config.yaml already exists. Do you want to overwrite it? (y/n): " overwrite
    if [ "$overwrite" != "y" ]; then
        echo "Exiting without changes."
        exit 0
    fi
fi

# Copy example config
cp config.yaml.example config.yaml

# Prompt for NGROK authtoken
read -p "Enter your NGROK authtoken: " ngrok_authtoken
if [ -z "$ngrok_authtoken" ]; then
    echo "NGROK authtoken is required. Please get one from https://dashboard.ngrok.com/"
    exit 1
fi

# Update NGROK authtoken in config.yaml
sed -i "s/authtoken: \"your_ngrok_authtoken_here\"/authtoken: \"$ngrok_authtoken\"/" config.yaml

# Prompt for provider API key
read -p "Enter your provider API key (leave empty to use default): " provider_api_key
if [ ! -z "$provider_api_key" ]; then
    sed -i "s/api_key: \"your_api_key_here\"/api_key: \"$provider_api_key\"/" config.yaml
fi

# Prompt for provider URL
read -p "Enter your provider URL (leave empty to use default): " provider_url
if [ ! -z "$provider_url" ]; then
    sed -i "s|url: \"http://localhost:80\"|url: \"$provider_url\"|" config.yaml
fi

# Prompt for provider type
read -p "Enter your provider type (leave empty to use default 'ollama'): " provider_type
if [ ! -z "$provider_type" ]; then
    sed -i "s/provider_type: \"ollama\"/provider_type: \"$provider_type\"/" config.yaml
fi

# Prompt for LLM URL
read -p "Enter your LLM URL (leave empty to use default 'http://localhost:11434'): " llm_url
if [ ! -z "$llm_url" ]; then
    sed -i "s|llm_url: \"http://localhost:11434\"|llm_url: \"$llm_url\"|" config.yaml
fi

# Prompt for server port
read -p "Enter your server port (leave empty to use default '8080'): " server_port
if [ ! -z "$server_port" ]; then
    sed -i "s/port: 8080/port: $server_port/" config.yaml
fi

echo "config.yaml has been created successfully!"
echo "You can now run the Docker container with:"
echo "docker run -d --name inferoute-client -p 8080:8080 -p 4040:4040 -v \$(pwd)/config.yaml:/app/config.yaml inferoute-client" 