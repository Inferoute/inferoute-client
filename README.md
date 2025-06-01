# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on vllm or Ollama provider machines. It handles health monitoring, reporting, and inference request handling.


We will also add support for exo-labs and llama.cppp in the future. 

🔥 What do we do?







## Requirements

- A user and provider setup on Inferoute.com [How to add a provider](https://github.com/inferoute/inferoute-client/blob/main/docs/provider.md) 
- Ollama or vllm running locally
- 🚨 Post installation 
    - When your client first starts it will publish your available models and add costs based on the average costs across all providers. 
    - Please remember to log on and change the costs to your preference if you prefer.

## Optional
- NVIDIA GPU with nvidia-smi installed (for GPU monitoring)
- AMD GPU with xxxxxx installed (for GPU monitoring)


## 💾 Installation

### Quick Start with Installation Script

### Prerequisites
- Get your API key from the [Inferoute platform](https://core.inferoute.com)

### Linux/macOS One-liner Installation
```bash
PROVIDER_API_KEY="your-key" curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | \
bash
```

### Manual Environment Variables
```bash
export PROVIDER_API_KEY="your-provider-api-key"
export PROVIDER_TYPE="ollama"  # or "vllm"
export LLM_URL="http://localhost:11434"  # or "http://localhost:8000" for vllm
export SERVER_PORT="8080"

# Then run the install script
curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | bash
```

### Windows

Please make sure to run you command prompt with administrator privileges

```ps
powershell -Command "& {iwr -useb https://raw.githubusercontent.com/sentnl/inferoute-client/main/scripts/windows-install.bat -OutFile windows-install.bat}" && windows-install.bat
```
[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)

### Docker

The official Inferoute Docker image inferoute/inferoute-client is available on Docker Hub. 

Please note if running Inferoute within Docker you need to ensure your Ollama instance is running on port 0.0.0.0 (This allows the Docker container to access the Ollama Server - [See Ollama guide for help](https://github.com/inferoute/inferoute-client/blob/main/docs/ollama.md))

We set the LLM_URL to http://host.docker.internal (resolves to the internal IP address used by the Docker host)


### Quick Start
```bash
docker run -d \
  --name inferoute-client \
  -p 8080:8080 \
  -e PROVIDER_API_KEY="your-key" \
  -e PROVIDER_TYPE="ollama" \
  -e LLM_URL="http://host.docker.internal:11434" \
  inferoute/inferoute-client:latest
```

### Docker Compose
```yaml
version: '3.8'
services:
  inferoute-client:
    image: inferoute/inferoute-client:latest
    ports:
      - "8080:8080"
    environment:
      - PROVIDER_API_KEY=your-key
      - PROVIDER_TYPE=ollama
      - LLM_URL=http://host.docker.internal:11434
    restart: unless-stopped
```

### Build from Source
```bash
docker build -t inferoute-client .
docker run -d \
  --name inferoute-client \
  -p 8080:8080 \
  -e PROVIDER_API_KEY="your-key" \
  inferoute-client
```

## 🚀 Launch Inferoute-client 

**INFEROUTE Start Command:**
`inferoute-client`

**INFEROUTE Start with specific config:**
`inferoute-client --config /home/charles/.config/inferoute/config.yaml`


## 💾 Post Installation

When your client first starts it will publish your available models with default costs. 
Please rememeber to log on and change the costs to your preference.

## 🎓 REST API 

- **GET /api/health**: Returns the current health status of the provider, including GPU information (if available) and available LLM models.
- **GET /api/busy**: Returns whether the GPU is currently busy (TRUE or FALSE).


## 📝 Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host) to access rest API's. 
- **provider**: Provider configuration (API key, central system URL)
  - **provider_type**: Type of LLM provider being used (default: "ollama", future support for "exo-labs" and "llama.cpp")
  - **llm_url**: URL of the local LLM provider API (default: "http://localhost:11434")
- **cloudflare**: Cloudflare tunnel configuration
  - **service_url**: Local service URL to tunnel (defaults to llm_url if not specified)
- **logging**: Logging configuration
  - **level**: Log level (debug, info, warn, error)
  - **log_dir**: Directory where logs are stored (defaults to ~/.local/state/inferoute/log)
  - **max_size**: Maximum size of log files in megabytes before rotation (default: 100)
  - **max_backups**: Maximum number of old log files to retain (default: 5)
  - **max_age**: Maximum number of days to retain old log files (default: 30)




