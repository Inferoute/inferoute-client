## Overview

The Provider Client is a lightweight Go service that runs on Ollama provider machines. It is implemented as a single Go application with the following components:

- Configuration management (`pkg/config`)
- GPU monitoring (`pkg/gpu`)
- Health reporting (`pkg/health`)
- Ollama API client (`pkg/ollama`)
- HTTP server (`pkg/server`)
- Structured logging (`pkg/logger`)

### Health Monitoring & Reporting:

1. It collects local metrics (e.g., GPU type, number of GPUs, utilization stats, models available) and reports them to the central system. It sends these reports to `http://localhost:80/api/provider/health`.

   - On Linux systems with NVIDIA GPUs, details are collected using `nvidia-smi -x -q` command and parsing the XML output to extract GPU type, driver version, CUDA version, memory usage, and utilization.
   - On macOS systems, GPU details are collected using `system_profiler SPDisplaysDataType` command to extract GPU model and core count information.
   - Available models are retrieved from the local Ollama instance via the REST API by calling `/api/models`.
   - The health report includes GPU information, available models, and the NGROK URL.

   Health reports are sent every 5 minutes automatically, and there is also an API endpoint `/health` that returns this information on demand.

2. The client exposes an API endpoint `/busy` that checks whether the GPU is busy:
   - On Linux systems, this is determined by examining the GPU utilization. If utilization is above 20%, the GPU is considered busy.
   - On macOS systems, the GPU is always considered not busy.
   
   This endpoint returns a JSON response with a boolean `busy` field.

### Model Pricing Initialization:

Before starting the health reporting cycle, the client performs a one-time initialization of model pricing:

1. **Model Discovery**:
   - Queries the local Ollama instance to get a list of all available models
   - Uses full model IDs including tags (e.g., "llama2:latest")

2. **Price Fetching**:
   - Makes a request to `/api/model-pricing/get-prices` with the list of discovered models
   - The response includes:
     - Model-specific pricing (if available)
     - Default pricing for models without specific pricing
   ```json
   {
     "model_prices": [
       {
         "model_name": "llama2",
         "avg_input_price": 0.0005,
         "avg_output_price": 0.0005,
         "sample_size": 4
       },
       {
         "model_name": "default",
         "avg_input_price": 0.0005,
         "avg_output_price": 0.0005,
         "sample_size": 1
       }
     ]
   }
   ```

3. **Price Registration**:
   - For each local model:
     - If model-specific pricing exists in the API response, uses those prices
     - If no model-specific pricing exists, uses the default pricing from the API response
     - Registers the model and its pricing via POST to `/api/provider/models`
   ```json
   {
     "model_name": "llama2",
     "service_type": "ollama",
     "input_price_tokens": 0.0005,
     "output_price_tokens": 0.0005
   }
   ```
   - Gracefully handles cases where models already exist (HTTP 409 Conflict)

4. **Error Handling**:
   - Continues operation even if pricing initialization fails
   - Logs detailed information about pricing decisions and any errors
   - Uses structured logging to track which pricing (specific or default) is used for each model

This initialization ensures that all local models are registered with appropriate pricing before the client begins handling inference requests.

Note that the API to register models with `/api/provider/models` does not allow updates to models - can only be used for initial creation of models.

### Automatic Model Registration:

The provider client now includes automatic registration of new models that are added to Ollama after the application has started:

1. **Model Tracking**:
   - The client maintains a registry of models that have already been registered with pricing
   - This registry is initialized at startup with the list of models registered during initialization
   - The registry is thread-safe to handle concurrent access

2. **Periodic Model Discovery**:
   - During each health report cycle (every 5 minutes), the client checks for new models
   - It compares the current list of models from Ollama with the tracked registry
   - Any models that exist in Ollama but not in the registry are identified as new models

3. **Automatic Registration**:
   - For each newly discovered model, the client:
     - Fetches pricing information from the central system
     - Uses model-specific pricing if available, or falls back to default pricing
     - Registers the model with the central system via `/api/provider/models`
     - Adds the model to the tracked registry upon successful registration
   
4. **Error Handling**:
   - If a model already exists (HTTP 400 Bad Request), it's marked as registered in the local registry
   - Registration failures are logged but don't interrupt the health reporting process
   - Detailed logging provides visibility into the registration process

This automatic registration ensures that any models added to Ollama after the client has started will be properly registered with pricing information without requiring a restart of the provider client.

### Inference Request Handling:

When an inference request is received from the central orchestrator, the provider client:

1. First determines whether its GPU is currently busy by checking the GPU utilization.
   - If the GPU is busy:
     The service immediately responds with a 503 Service Unavailable status and a JSON error message, allowing the orchestrator to try another provider.
   - If the GPU is available:
     It proceeds to the next step.

2. Validates the HMAC if present in the request header (`X-Request-Id`).
   - The HMAC is validated by sending a request to `/api/provider/validate_hmac` on the central system.
   - If validation fails, the request is rejected with a 401 Unauthorized status.

3. If validation succeeds, the request is forwarded to the local Ollama instance and the response is returned to the client.
   - The client supports both `/v1/chat/completions` and `/v1/completions` endpoints, making it compatible with the OpenAI API.

### Logging System:

The provider client implements a comprehensive logging system using Zap, a high-performance structured logging library:

1. **Structured Logging**: All logs include structured fields (method, path, status, duration, etc.) for better filtering and analysis.

2. **Log Levels**: Supports multiple log levels (debug, info, warn, error) configurable via the configuration file.

3. **Log Rotation**: Uses lumberjack for automatic log rotation based on:
   - Maximum file size (default: 100MB)
   - Maximum number of backups (default: 5)
   - Maximum age of log files (default: 30 days)
   - Compression of old log files

4. **Multiple Outputs**:
   - Console output for real-time monitoring
   - JSON-formatted file logs for machine parsing
   - Separate error log file for critical issues

5. **Performance Optimized**: Zap is designed for high-throughput services with minimal overhead, ensuring logging doesn't impact request latency.

### Console Interface:

The provider client features a real-time console interface that displays:

1. **System Information**:
   - Last health update timestamp
   - Session status
   - Provider configuration details
   - NGROK URL (if configured)

2. **GPU Information**:
   - GPU model
   - Driver version
   - CUDA version
   - GPU count

3. **Request Monitoring**:
   - Recent requests with timestamp, method, path, status code, and formatted duration
   - Error logs

The console interface refreshes every 3 seconds to provide up-to-date information without excessive flickering.

### NGROK Integration:

The provider client includes NGROK integration to create a secure tunnel back to the central system. This allows the provider's local inference engine (Ollama server) to be accessible via a public URL without exposing the machine directly.

Key features of the NGROK integration:

1. **Dynamic URL Discovery**:
   - Instead of hardcoding the NGROK URL, the client dynamically fetches it from the local NGROK API
   - On startup, it connects to `http://localhost:{port}/api/tunnels` (default port: 4040)
   - It extracts the first available HTTP/HTTPS tunnel URL
   - This URL is then used for all communications with the central system

2. **Automatic Updates**:
   - The NGROK URL is refreshed each time a health report is generated (every 5 minutes)
   - If the URL changes (e.g., due to NGROK restart or reconnection), the new URL is automatically detected
   - This ensures the system always uses the current valid tunnel URL

3. **Configuration**:
   - Only the NGROK API port and authentication token need to be configured
   - The URL is automatically discovered and doesn't need to be manually updated

4. **Resilience**:
   - If the NGROK API is temporarily unavailable, the client continues using the last known URL
   - Detailed logging of URL discovery attempts and failures
   - Graceful handling of cases where no tunnels are available

This dynamic approach eliminates the need to manually update configuration files when NGROK URLs change, making the system more robust and easier to maintain.

### Configuration:

The application reads from a YAML configuration file with the following sections:

- `server`: Server configuration (port, host)
- `provider`: Provider configuration (API key, central system URL, provider type, LLM URL)
- `ngrok`: NGROK configuration (authtoken, port)
- `logging`: Logging configuration (level, directory, rotation settings)

A default configuration is provided if the file is not found. The NGROK URL is dynamically fetched from the local NGROK API rather than being hardcoded.

## Key Components

### 1. GPU Monitor (`pkg/gpu/monitor.go`)

- Detects the operating system and uses the appropriate GPU monitoring method:
  - On Linux: Uses `nvidia-smi` for NVIDIA GPUs
  - On macOS: Uses `system_profiler SPDisplaysDataType` for Apple GPUs
- Retrieves GPU information including product name, driver version, CUDA version, memory usage, and utilization (where available)
- On Linux, determines if the GPU is busy based on utilization threshold (20%)
- On macOS, always reports the GPU as not busy
- Gracefully handles cases where GPU information cannot be obtained
- Logs GPU status and errors using structured logging

### 2. Health Reporter (`pkg/health/reporter.go`)

- Collects GPU information and available models
- Creates and sends health reports to the central system
- Provides an endpoint to retrieve the current health report
- Tracks the last successful health update time
- Handles cases where GPU information is not available
- Logs health reporting activities and errors

### 3. Ollama Client (`pkg/ollama/client.go`)

- Communicates with the local Ollama instance
- Lists available models
- Sends chat and completion requests
- Logs API interactions with detailed request/response information

### 4. HTTP Server (`pkg/server/server.go`)

- Exposes API endpoints for health checks, busy status, and inference requests
- Handles HMAC validation
- Forwards requests to Ollama after validation
- Provides a real-time console interface
- Handles cases where GPU monitoring is not available
- Logs requests with formatted durations and status codes

### 5. Configuration (`pkg/config/config.go`)

- Loads configuration from YAML file
- Provides default values if configuration is not found
- Includes logging configuration options

### 6. Logger (`pkg/logger/logger.go`)

- Implements structured logging using Zap
- Configures log rotation using lumberjack
- Provides helper methods for different log levels
- Supports multiple output destinations

### 7. NGROK Client (`pkg/ngrok/client.go`)

- Dynamically fetches the current NGROK URL from the local NGROK API
- Automatically detects and adapts to URL changes
- Provides resilient handling of API connection issues
- Supports configurable API port
- Logs detailed information about URL discovery and updates

## API Endpoints

1. `GET /health`: Returns the current health status including GPU information and available models
2. `GET /busy`: Returns whether the GPU is currently busy
3. `POST /v1/chat/completions`: OpenAI-compatible chat completions API endpoint
4. `POST /v1/completions`: OpenAI-compatible completions API endpoint

## Cross-Platform Support

The provider client is designed to work on both Linux and macOS systems:

1. **Linux with NVIDIA GPUs**:
   - Full GPU monitoring with detailed information
   - Accurate busy status based on GPU utilization

2. **macOS with Apple GPUs**:
   - Basic GPU information (model name, core count)
   - Always reports GPU as not busy
   - Limited memory and utilization information

3. **Systems without GPU monitoring**:
   - Client continues to function without GPU information
   - Reports empty/null GPU data in health reports
   - Always reports GPU as not busy

## Deployment

The provider client is built in Go and can be packaged in a Docker container for easy deployment. It is designed to be as small and efficient as possible, with minimal dependencies.

## Logging Configuration

The logging system can be configured in the `config.yaml` file:

```yaml
logging:
  # Log level: debug, info, warn, error
  level: "info"
  # Log directory (defaults to ~/.local/state/inferoute/log if empty)
  log_dir: ""
  # Maximum size of log files in megabytes before rotation
  max_size: 100
  # Maximum number of old log files to retain
  max_backups: 5
  # Maximum number of days to retain old log files
  max_age: 30
```









