## Overview

The Provider Client is a lightweight Go service that runs on Ollama provider machines. It is implemented as a single Go application with the following components:

- Configuration management (`pkg/config`)
- GPU monitoring (`pkg/gpu`)
- Health reporting (`pkg/health`)
- Ollama API client (`pkg/ollama`)
- HTTP server (`pkg/server`)

### Health Monitoring & Reporting:

1. It collects local metrics (e.g., GPU type, number of GPUs, utilization stats, models available) and reports them to the central system. It sends these reports to `http://localhost:80/api/provider/health`.

   - GPU details are collected using `nvidia-smi -x -q` command and parsing the XML output to extract GPU type, driver version, CUDA version, memory usage, and utilization.
   - Available models are retrieved from the local Ollama instance via the REST API by calling `/api/models`.
   - The health report includes GPU information, available models, and the NGROK URL.

   Health reports are sent every 5 minutes automatically, and there is also an API endpoint `/health` that returns this information on demand.

2. The client exposes an API endpoint `/busy` that checks whether the GPU is busy by examining the GPU utilization. If utilization is above 20%, the GPU is considered busy. This endpoint returns a JSON response with a boolean `busy` field.

### Inference Request Handling:

When an inference request is received from the central orchestrator, the provider client:

1. First determines whether its GPU is currently busy by checking the GPU utilization.
   - If the GPU is busy:
     The service immediately responds with a 503 Service Unavailable status and a JSON error message, allowing the orchestrator to try another provider.
   - If the GPU is available:
     It proceeds to the next step.

2. Validates the HMAC if present in the request header (`X-Inferoute-HMAC`).
   - The HMAC is validated by sending a request to `/api/provider/validate_hmac` on the central system.
   - If validation fails, the request is rejected with a 401 Unauthorized status.

3. If validation succeeds, the request is forwarded to the local Ollama instance and the response is returned to the client.
   - The client supports both `/v1/chat/completions` and `/v1/completions` endpoints, making it compatible with the OpenAI API.

### NGROK Integration:

The provider client includes NGROK URL configuration to create a secure tunnel back to the central system. This allows the provider's local inference engine (Ollama server) to be accessible via a public URL without exposing the machine directly. This public URL is included in the health report data.

### Configuration:

The application reads from a YAML configuration file with the following sections:

- `server`: Server configuration (port, host)
- `ollama`: Ollama configuration (URL, defaults to `http://localhost:11434`)
- `provider`: Provider configuration (API key, central system URL)
- `ngrok`: NGROK configuration (URL)

A default configuration is provided if the file is not found, and the NGROK URL is currently hardcoded as specified in the requirements.

## Key Components

### 1. GPU Monitor (`pkg/gpu/monitor.go`)

- Checks for the availability of `nvidia-smi` on startup
- Retrieves GPU information including product name, driver version, CUDA version, memory usage, and utilization
- Determines if the GPU is busy based on utilization threshold (20%)

### 2. Health Reporter (`pkg/health/reporter.go`)

- Collects GPU information and available models
- Creates and sends health reports to the central system
- Provides an endpoint to retrieve the current health report

### 3. Ollama Client (`pkg/ollama/client.go`)

- Communicates with the local Ollama instance
- Lists available models
- Sends chat and completion requests

### 4. HTTP Server (`pkg/server/server.go`)

- Exposes API endpoints for health checks, busy status, and inference requests
- Handles HMAC validation
- Forwards requests to Ollama after validation

### 5. Configuration (`pkg/config/config.go`)

- Loads configuration from YAML file
- Provides default values if configuration is not found

## API Endpoints

1. `GET /health`: Returns the current health status including GPU information and available models
2. `GET /busy`: Returns whether the GPU is currently busy
3. `POST /v1/chat/completions`: OpenAI-compatible chat completions API endpoint
4. `POST /v1/completions`: OpenAI-compatible completions API endpoint

## Deployment

The provider client is built in Go and can be packaged in a Docker container for easy deployment. It is designed to be as small and efficient as possible, with minimal dependencies.









