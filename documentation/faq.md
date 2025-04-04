# Inferoute Provider Client FAQ

## General Questions

### What is the Inferoute Provider Client?
The Inferoute Provider Client is a lightweight Go service that runs on Ollama provider machines. It monitors GPU resources, reports health metrics to the central system, and handles inference requests by forwarding them to the local Ollama instance.

### What platforms does the Provider Client support?
The Provider Client supports both Linux systems with NVIDIA GPUs and macOS systems with Apple GPUs. It has different capabilities on each platform:
- **Linux with NVIDIA GPUs**: Full GPU monitoring with detailed information and accurate busy status based on GPU utilization
- **macOS with Apple GPUs**: Basic GPU information (model name, core count) with limited memory and utilization information

### How do I configure the Provider Client?
The Provider Client reads from a YAML configuration file with sections for server settings, provider details, NGROK configuration, and logging options. A default configuration is provided if the file is not found.

## Model Management

### When I add new models to Ollama, will the Inferoute Client automatically pick this up?
Yes, the Inferoute Client will automatically pick up new models at every health check, which happens every 5 minutes. It will also automatically set pricing for the new model based on average pricing via the Inferoute-node API.

### How does model pricing work?
The Provider Client performs a one-time initialization of model pricing at startup:
1. It discovers all available models from the local Ollama instance
2. Fetches pricing information from the central system
3. Registers each model with either model-specific pricing or default pricing
4. New models added after startup are automatically detected and registered during health checks

### Can I update model pricing after registration?
Yes you can use the /api/provider/models/{model_id} API - see documentation here 
You can also log onto to your user profile and make changes to model pricing.

## NGROK Integration

### How does the NGROK URL get updated?
The Provider Client dynamically discovers and updates the NGROK URL:
1. On startup, it connects to the local NGROK API (default: http://localhost:4040/api/tunnels)
2. It extracts the first available HTTP/HTTPS tunnel URL
3. The URL is refreshed each time a health report is generated (every 5 minutes)
4. If the URL changes (e.g., due to NGROK restart), the new URL is automatically detected without requiring manual updates

### Do I need to manually update the NGROK URL in configuration?
No, you don't need to manually update the NGROK URL. The client dynamically fetches it from the local NGROK API rather than using a hardcoded value. You only need to configure the NGROK API port and authentication token.

### What happens if the NGROK API is temporarily unavailable?
If the NGROK API is temporarily unavailable, the client continues using the last known URL. It has graceful handling for cases where no tunnels are available and provides detailed logging of URL discovery attempts and failures.

## Health Monitoring

### How often does the Provider Client report health metrics?
The Provider Client sends health reports to the central system every 5 minutes automatically. It also exposes an API endpoint `/health` that returns this information on demand.

### What information is included in health reports?
Health reports include:
- GPU information (type, driver version, CUDA version, memory usage, utilization)
- Available models from the local Ollama instance
- The current NGROK URL

### How does the Provider Client determine if the GPU is busy?
- On Linux systems with NVIDIA GPUs, the GPU is considered busy if utilization is above 20%
- On macOS systems, the GPU is always considered not busy
- Systems without GPU monitoring always report the GPU as not busy

## Inference Requests

### How does the Provider Client handle inference requests?
When an inference request is received:
1. It checks if the GPU is busy
2. If busy, it responds with a 503 Service Unavailable status
3. If available, it validates the HMAC (if present)
4. If validation succeeds, it forwards the request to the local Ollama instance

### What API endpoints does the Provider Client support?
The Provider Client supports OpenAI-compatible endpoints:
- `POST /v1/chat/completions`: For chat completions
- `POST /v1/completions`: For standard completions

### How does the Provider Client validate requests?
The Provider Client validates requests by checking the HMAC in the request header (`X-Request-Id`). The HMAC is validated by sending a request to `/api/provider/validate_hmac` on the central system.

## Logging

### How does the logging system work?
The Provider Client implements a comprehensive logging system using Zap:
- Structured logging with multiple fields for better filtering
- Multiple log levels (debug, info, warn, error)
- Automatic log rotation based on file size, number of backups, and age
- Multiple output destinations (console and JSON-formatted files)

### How can I configure logging?
Logging can be configured in the `config.yaml` file with options for:
- Log level (debug, info, warn, error)
- Log directory
- Maximum file size before rotation
- Maximum number of backups
- Maximum age of log files

## Troubleshooting

### What happens if GPU monitoring is not available?
If GPU monitoring is not available, the Provider Client:
- Continues to function without GPU information
- Reports empty/null GPU data in health reports
- Always reports the GPU as not busy

### How can I monitor the Provider Client's activity?
The Provider Client features a real-time console interface that displays:
- System information (last health update, session status, configuration details)
- GPU information (model, driver version, CUDA version, count)
- Request monitoring (recent requests with timestamp, method, path, status code)
- Error logs

The console interface refreshes every 3 seconds to provide up-to-date information. 