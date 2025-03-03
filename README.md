# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on Ollama provider machines. It handles health monitoring, reporting, and inference request handling.

## Features

- **Health Monitoring & Reporting**: Collects local metrics (GPU type, number of GPUs, utilization stats, models available) and reports them to the central system.
- **Inference Request Handling**: Forwards inference requests to the local Ollama instance after checking GPU availability.
- **HMAC Validation**: Validates HMACs on incoming requests to ensure they are legitimate.
- **OpenAI API Compatibility**: Implements the OpenAI API for chat completions and completions.

## Requirements

- NVIDIA GPU with nvidia-smi installed (for GPU monitoring)
- Ollama running locally
- jq (installed automatically by the script if missing)

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
~/inferoute-client/run/start.sh
```
[Manual install instructions](https://github.com/inferoute/inferoute-client/blob/main/docs/linux.md)

### Windows

Please mak sure to run you command prompt with administrator privileges

```ps
powershell -Command "& {iwr -useb https://raw.githubusercontent.com/sentnl/inferoute-client/main/scripts/windows-install.bat -OutFile windows-install.bat}" && windows-install.bat
```

### Docker

The official Ollama Docker image ollama/ollama is available on Docker Hub.


## REST API 

- **GET /health**: Returns the current health status of the provider, including GPU information and available models.
- **GET /busy**: Returns whether the GPU is currently busy (TRUE or FALSE).


## Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host)
- **ollama**: Ollama configuration (URL)
- **provider**: Provider configuration (API key, central system URL)
- **ngrok**: NGROK configuration (URL, authtoken)




