# Inferoute Client Docker Setup

This directory contains the Docker setup for running the Inferoute Client with NGROK integration.

## Prerequisites

Before running the Docker container, you need to create a `config.yaml` file based on the provided `config.yaml.example`. The most important part is to add your NGROK authtoken in the config file:

```yaml
# NGROK configuration
ngrok:
  url: ""  # This will be automatically updated by the container
  authtoken: "your_ngrok_authtoken_here"  # Replace with your actual NGROK authtoken
```

### Easy Configuration Setup

We provide a helper script to create your config.yaml file:

```bash
# Make the script executable
chmod +x docker/create-config.sh

# Run the script
./docker/create-config.sh
```

The script will prompt you for the necessary configuration values and create a config.yaml file for you.

## Building the Docker Image

```bash
docker build -t inferoute-client .
```

## Running the Docker Container

```bash
docker run -d \
  --name inferoute-client \
  -p 8080:8080 \
  -p 4040:4040 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  inferoute-client
```

## Configuration

The container reads all configuration from the `config.yaml` file. You need to prepare this file before running the container with the following sections:

1. **Server Configuration**: Port and host for the Inferoute server
2. **Provider Configuration**: API key, URL, provider type, and LLM URL (only Ollama supported for now)
3. **NGROK Configuration**: Your NGROK authtoken (required)
4. **Logging Configuration**: Log level, directory, and rotation settings

Example:

```yaml
# Server configuration
server:
  port: 8080
  host: "0.0.0.0"

# Provider configuration
provider:
  api_key: "your_provider_api_key_here"
  url: "http://your_provider_url_here"
  provider_type: "ollama"
  llm_url: "http://your_ollama_url:11434"

# NGROK configuration
ngrok:
  url: ""  # Will be automatically updated by the container
  authtoken: "your_ngrok_authtoken_here"

# Logging configuration
logging:
  level: "info"
  log_dir: ""
  max_size: 100
  max_backups: 5
  max_age: 30
```

## Accessing the Services

- Inferoute Client API: http://localhost:8080
- NGROK Admin Interface: http://localhost:4040
- NGINX Proxy: http://localhost:80
  - Inferoute Client API: http://localhost/
  - NGROK Admin Interface: http://localhost/ngrok/

## How It Works

1. The container reads your `config.yaml` file and extracts the NGROK authtoken.
2. NGROK creates a public tunnel to the Inferoute Client.
3. The NGROK public URL is automatically updated in your `config.yaml` file.
4. The Inferoute Client uses the NGROK URL for external access.
5. NGINX proxies requests to both the Inferoute Client and NGROK admin interface.

## Security Considerations

- The Docker container is configured to run with minimal privileges where possible.
- Sensitive information like your NGROK authtoken should be kept secure.
- The NGROK tunnel exposes your local service to the internet, so ensure your API is properly secured.
- Consider using HTTPS for production deployments.
- The container uses a multi-stage build to minimize the attack surface.

## Troubleshooting

If you encounter issues:

1. Check the logs: `docker logs inferoute-client`
2. Verify your NGROK authtoken is correct
3. Make sure your config.yaml is properly formatted
4. Check if the required ports are available on your host machine

## Volumes

You can mount your own config.yaml file to override the default configuration:

```bash
-v /path/to/your/config.yaml:/app/config.yaml
``` 