# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on Ollama provider machines. It handles health monitoring, reporting, and inference request handling.

## Features

- **Health Monitoring & Reporting**: Collects local metrics (GPU type, number of GPUs, utilization stats, models available) and reports them to the central system.
- **Inference Request Handling**: Forwards inference requests to the local Ollama instance after checking GPU availability.
- **HMAC Validation**: Validates HMACs on incoming requests to ensure they are legitimate.
- **OpenAI API Compatibility**: Implements the OpenAI API for chat completions and completions.

## Requirements

- Go 1.21 or higher
- NVIDIA GPU with nvidia-smi installed
- Ollama running locally

## Installation

1. Clone the repository:
   ```
   git clone https://github.com/sentnl/inferoute-client.git
   cd inferoute-client
   ```

2. Copy the example configuration file:
   ```
   cp config.yaml.example config.yaml
   ```

3. Edit the configuration file to set your provider API key and other settings:
   ```
   nano config.yaml
   ```

4. Build the client:
   ```
   go build -o inferoute-client ./cmd
   ```

## Usage

1. Start the client:
   ```
   ./inferoute-client
   ```

   Or with a custom configuration file:
   ```
   ./inferoute-client -config /path/to/config.yaml
   ```

2. The client will start a server on the configured port (default: 8080) and begin sending health reports to the central system.

## API Endpoints

- **GET /health**: Returns the current health status of the provider, including GPU information and available models.
- **GET /busy**: Returns whether the GPU is currently busy (TRUE or FALSE).
- **POST /v1/chat/completions**: OpenAI-compatible chat completions API endpoint.
- **POST /v1/completions**: OpenAI-compatible completions API endpoint.

## Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host)
- **ollama**: Ollama configuration (URL)
- **provider**: Provider configuration (API key, central system URL)
- **ngrok**: NGROK configuration (URL)

## License

This project is licensed under the MIT License - see the LICENSE file for details. 
