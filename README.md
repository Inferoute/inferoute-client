# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on vllm or Ollama provider machines. It handles health monitoring, reporting, and inference request handling.


We will also add support for exo-labs and llama.cppp in the future. 

🔥 What do we do?




## Supported platforms

| Platform | GPU | LLM backend | Notes |
|----------|-----|-------------|-------|
| **Linux + NVIDIA** | Full monitoring via `nvidia-smi` | Ollama or vLLM | Recommended for production providers |
| **macOS** (Intel or Apple Silicon) | Basic info via `system_profiler` | Ollama (typical) | GPU busy always reported as false; no memory or utilization metrics |
| **Windows amd64** | `nvidia-smi` when the NVIDIA driver is installed | **Ollama** | Native `.exe`. vLLM is not supported on Windows. Optional `--tray` runs in the notification area |

## Requirements

- A user and provider setup on Inferoute.com [How to add a provider](https://github.com/inferoute/inferoute-client/blob/main/docs/provider.md)
- Ollama or vLLM running locally (Ollama is typical on macOS)
- **Linux with NVIDIA GPU:** `nvidia-smi` must be installed and available on `PATH` (required for GPU monitoring and busy-state detection). The install script does not install it; install the [NVIDIA driver](https://www.nvidia.com/drivers) for your system.
- **macOS with Apple GPU:** Ollama running locally. The install script supports Intel and Apple Silicon Macs and installs `cloudflared` via Homebrew when available.
- **Windows amd64:** [Ollama for Windows](https://ollama.com). The install script installs `cloudflared` and `inferoute-client`, and adds them to your user PATH. `nvidia-smi` is required for GPU monitoring (install the [NVIDIA driver](https://www.nvidia.com/drivers)); without it the client still runs. vLLM is not supported on native Windows.
- 🚨 Post installation
    - When your client first starts it will publish your available models and add costs based on the average costs across all providers.
    - Please remember to log on and change the costs to your preference if you prefer.

## Exposing your machine via Cloudflare (secure HTTPS)

The client **exposes your machine to the internet** so the Inferoute platform can send inference requests to your local Ollama or vLLM server. This is done securely using **Cloudflare Tunnel** (cloudflared):

- **Secure & HTTPS:** Traffic between the internet and your provider goes through Cloudflare's network over HTTPS. Your home IP and ports are not exposed; Cloudflare provides a stable, TLS-terminated URL which tunnels back to your IP.
- **Why we install cloudflared:** The install script installs the `cloudflared` binary on your host so the client can run it automatically. When you start the client, it requests a tunnel from the Inferoute platform, then starts and supervises the cloudflared process. You do not need to run or configure cloudflared yourself—the client manages the tunnel for you.
- **No open firewall ports** are required on your side; outbound HTTPS to Cloudflare is sufficient.


## 💾 Installation

### Quick Start with Installation Script

### Prerequisites
- Get your API key from the [Inferoute platform](https://core.inferoute.com)

### Linux / macOS one-liner

Works on Linux (amd64/arm64) and macOS (Intel and Apple Silicon):

```bash
PROVIDER_API_KEY="your-key" curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | bash
```

On macOS, the script installs `cloudflared` via Homebrew when available, otherwise downloads the native binary for your architecture.

### Windows (PowerShell)

Requires 64-bit Windows. Ollama should already be installed.

```powershell
$env:PROVIDER_API_KEY="your-key"; irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
```

Optional: `$env:PROVIDER_TYPE="ollama"`, `$env:LLM_URL="http://localhost:11434"`, `$env:SERVER_PORT="8080"`.

The script installs `cloudflared` and `inferoute-client` to `%LOCALAPPDATA%\inferoute\bin`, writes `%USERPROFILE%\.config\inferoute\config.yaml`, and adds a **Start Menu → Inferoute → Inferoute Client** shortcut that runs with `--tray`. See [docs/windows.md](docs/windows.md).

Or download `scripts/windows-install.bat` and double-click it (no administrator prompt).

### Manual Environment Variables
```bash
export PROVIDER_API_KEY="your-provider-api-key"
export PROVIDER_TYPE="ollama"  # or "vllm"
export LLM_URL="http://localhost:11434"  # or "http://localhost:8000" for vllm
export SERVER_PORT="8080"

# Then run the install script
curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | bash
```

[Override default parameters](https://github.com/inferoute/inferoute-client/blob/main/docs/override.md)


## 🚀 Launch Inferoute-client 

**INFEROUTE Start Command:**
`inferoute-client`

**INFEROUTE Start with specific config:**
`inferoute-client --config ~/.config/inferoute/config.yaml`

**Windows (notification area, no console):**
`inferoute-client --tray`

## Model compatibility check

Works on **Linux + NVIDIA** (`nvidia-smi`), **Windows + NVIDIA** (`nvidia-smi`), and **macOS** (Apple Silicon unified memory via `sysctl` / `system_profiler`). Does **not** start the provider daemon and does not need an API key.

```bash
inferoute-client compatibility
inferoute-client compatibility --provider-type ollama
inferoute-client compatibility --provider-type vllm
inferoute-client compatibility --json
inferoute-client compatibility --catalog-url https://core.inferoute.com
inferoute-client compatibility --offline-catalog ./approved-models.json
```

Statuses: `runs_well`, `fits`, `tight`, `too_large`, `unknown`. Scoring uses catalog `min_size_bytes` plus a conservative runtime overhead (higher for vLLM). Apple Silicon uses a fraction of unified system RAM; Linux scores against the largest single GPU’s VRAM.


## 📦 Docker Installation

The official Inferoute Docker image inferoute/inferoute-client is available on Docker Hub. 

Please note if running Inferoute within Docker you need to ensure your Ollama instance is running on port 0.0.0.0 (This allows the Docker container to access the Ollama Server - [See Ollama guide for help](https://github.com/inferoute/inferoute-client/blob/main/docs/ollama.md))

We set the LLM_URL to http://host.docker.internal (resolves to the internal IP address used by the Docker host)


### Docker Quick Start
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


## 💾 Post Installation

When your client first starts it will publish your available models with default costs. 
Please remember to visit inferoute.com and change the costs to your preference.

## 🎓 REST API 

- **GET /api/health**: Returns the current health status of the provider, including GPU information (if available) and available LLM models.
- **GET /api/busy**: Returns whether the GPU is currently busy (TRUE or FALSE).


## 📝 Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host) to access rest API's. 
- **provider**: Provider configuration (API key, central system URL)
  - **provider_type**: Type of LLM provider being used (default: "ollama", future support for "exo-labs" and "llama.cpp")
  - **llm_url**: URL of the local LLM provider API (default: "http://localhost:11434")
- **logging**: Logging configuration
  - **level**: Log level (debug, info, warn, error)
  - **log_dir**: Directory where logs are stored (defaults to ~/.local/state/inferoute/log)
  - **max_size**: Maximum size of log files in megabytes before rotation (default: 100)
  - **max_backups**: Maximum number of old log files to retain (default: 5)
  - **max_age**: Maximum number of days to retain old log files (default: 30)

**Log file locations** (under `log_dir`, default `~/.local/state/inferoute/log`):

- **inferoute.log** — main application log (all levels)
- **error.log** — error-level entries only




