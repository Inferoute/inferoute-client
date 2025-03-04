# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on Ollama provider machines. It handles health monitoring, reporting, and inference request handling.


We will also add support for exo-labs and llama.cppp in the future. 

## Features

- **Health Monitoring & Reporting**: Collects local metrics (GPU type, number of GPUs, utilization stats, models available) and reports them to the central system.
- **Inference Request Handling**: Forwards inference requests to the local Ollama instance after checking GPU availability.
- **HMAC Validation**: Validates HMACs on incoming requests to ensure they are legitimate.
- **OpenAI API Compatibility**: Implements the OpenAI API for chat completions and completions.

## Requirements

- A user and provider setup on Inferoute.com [How to add a provider](https://github.com/inferoute/inferoute-client/blob/main/docs/provider.md) 
- Ollama running locally
- jq (installed automatically by the script if missing)

## Optional

- NVIDIA GPU with nvidia-smi installed (for GPU monitoring)
- AMD GPU with xxxxxx installed (for GPU monitoring)



## Installation

### Linux/OSX

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

Please mak sure to run you command prompt with administrator privileges

```ps
powershell -Command "& {iwr -useb https://raw.githubusercontent.com/sentnl/inferoute-client/main/scripts/windows-install.bat -OutFile windows-install.bat}" && windows-install.bat
```
[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)

### Docker

The official Inferoute Docker image inferoute/inferoute-client is available on Docker Hub. 

Please note if running Inferoute within Docker you need to ensure your Ollama instance is running on port 0.0.0.0 (This allows the Docker container to access the Ollama Server - [See Ollama guide for help](https://github.com/inferoute/inferoute-client/blob/main/docs/ollama.md))


```bash
docker run -d --name inferoute-client \
  --add-host=host.docker.internal:host-gateway \
  -e NGROK_AUTHTOKEN="your-token" \
  -e PROVIDER_API_KEY="your-key" \
  -e LLM_URL="http://host.docker.internal:11434" \
  -p 8080:8080 -p 4040:4040 \
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
  -p 8080:8080 -p 4040:4040 \
  inferoute/inferoute-client:latest
```


[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)



## REST API 

- **GET /health**: Returns the current health status of the provider, including GPU information and available models.
- **GET /busy**: Returns whether the GPU is currently busy (TRUE or FALSE).


## Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host)
- **llm**: LLM configuration (URL)
- **provider**: Provider configuration (API key, central system URL)
- **ngrok**: NGROK configuration (URL, authtoken)




