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

### Linux/OSX

The defaults will connect to your vllm running on http://localhost:8000.

```bash
curl -fsSL https://raw.githubusercontent.com/Inferoute/inferoute-client/main/scripts/install.sh | \
  NGROK_AUTHTOKEN="your-token" \
  PROVIDER_API_KEY="your-key" \
  bash
```


After installation, start the client with:
```bash
inferoute-client 
```

[Manual install instructions](https://github.com/inferoute/inferoute-client/blob/main/docs/linux.md)

[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)

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


```bash
docker run -d --name inferoute-client \
  --add-host=host.docker.internal:host-gateway \
  -e NGROK_AUTHTOKEN="your-token" \
  -e PROVIDER_API_KEY="your-key" \
  -e LLM_URL="http://host.docker.internal:11434" \
  -p 8080:8080 \
  inferoute/inferoute-client:latest
```

#### To see your logs for you Inferoute-client on Docker 

```bash
sudo docker logs -f --since 1m inferoute-client 
```

### Docker upgrade

####  Pull the new image first.

```bash
sudo docker pull inferoute/inferoute-client:latest
```

#### Rerun original command 

```bash
docker run -d --name inferoute-client \
  --add-host=host.docker.internal:host-gateway \
  -e NGROK_AUTHTOKEN="your-token" \
  -e PROVIDER_API_KEY="your-key" \
  -e LLM_URL="http://host.docker.internal:11434" \
  -p 8080:8080  \
  inferoute/inferoute-client:latest
```


[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)

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
- **ngrok**: NGROK configuration
  - **authtoken**: Your NGROK authentication token
  - **port**: NGROK API port (default: 4040) - used to automatically fetch the current NGROK URL
- **logging**: Logging configuration
  - **level**: Log level (debug, info, warn, error)
  - **log_dir**: Directory where logs are stored (defaults to ~/.local/state/inferoute/log)
  - **max_size**: Maximum size of log files in megabytes before rotation (default: 100)
  - **max_backups**: Maximum number of old log files to retain (default: 5)
  - **max_age**: Maximum number of days to retain old log files (default: 30)




