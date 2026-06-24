## Overview

The Inferoute Provider Client is a Go service that runs on provider GPU machines alongside **Ollama** or **vLLM**. It:

- Exposes a local HTTP proxy for OpenAI-compatible inference
- Reports health to the Inferoute platform on a schedule
- Registers models and pricing with the platform
- Verifies models against the platform **approved-builds** catalog before routing traffic
- Requests and supervises a **Cloudflare Tunnel** (`cloudflared`) so Inferoute can reach the machine without open firewall ports

Entry point: `cmd/main.go`

### Package layout

| Package | Role |
|---------|------|
| `internal/config` | YAML configuration load and defaults |
| `pkg/server` | HTTP server, console UI, HMAC validation, request proxying |
| `pkg/health` | Health report assembly and push to platform |
| `pkg/llm` | Ollama / vLLM client abstraction (`ListModels`, `ForwardRequest`) |
| `pkg/gpu` | GPU monitoring (NVIDIA on Linux, basic info on macOS) |
| `pkg/cloudflare` | Tunnel request, `cloudflared` process supervision |
| `pkg/pricing` | Model price lookup and registration |
| `pkg/verify` | Approved-catalog fetch, local measurement, server-as-judge verification |
| `pkg/logger` | Zap structured logging with rotation |
| `pkg/usermsg` | User-facing error strings for console and HTTP |

## Startup sequence (`cmd/main.go`)

1. Load config from `--config` or `~/.config/inferoute/config.yaml`
2. Initialize logger, GPU monitor (optional), LLM client
3. Fetch public approved-builds catalog (`GET /api/models/approved-builds`)
4. Create verifier (`pkg/verify`) and register local models with pricing (`pkg/pricing`)
5. Start HTTP server (`pkg/server`):
   - Request tunnel from platform (`POST /api/cloudflare/tunnel/request`)
   - Start and supervise `cloudflared`
6. Start health reporter loop (`health.ReportInterval` = **3 minutes**):
   - Wait up to 30s for tunnel URL
   - Send initial health report, then on ticker

## Configuration (`internal/config`)

YAML sections:

- **server** — `port` (default 8080), `host` (default `0.0.0.0`)
- **provider** — `api_key`, `url` (Inferoute platform base URL), `provider_type` (`ollama` | `vllm`), `llm_url`, optional `hf_hub_cache` and `model_path` (vLLM weight resolution)
- **logging** — level, `log_dir`, rotation (`max_size`, `max_backups`, `max_age`)

`TunnelServiceURL()` derives the local URL passed to Cloudflare (`http://localhost:<port>` when host is `0.0.0.0`). There is no separate Cloudflare section in config.

## Health reporting (`pkg/health`)

### Interval

`ReportInterval = 3 * time.Minute` — health is pushed to `POST /api/provider/health` on the platform.

### Payload (`HealthReport`)

- `data` — models from local LLM, enriched with `verification_status`, digest/fingerprint fields
- `gpu` — product name, driver, CUDA, counts, memory, utilization (when available)
- `cloudflare` — `url` (tunnel hostname) only; **no client-side geolocation**
- `provider_type` — `ollama` or `vllm`

### Per health cycle

1. `RefreshCatalog` — reload approved-builds list; clears verify cache if catalog changed
2. `ListModels` from local LLM
3. `ApplyToModels` — verification (see below)
4. `registerNewModels` — register any newly verified models with pricing
5. `POST /api/provider/health` with Bearer provider API key

Platform-side: provider-management persists GPU/tunnel fields synchronously, publishes to RabbitMQ; **cluster country** is resolved asynchronously by cloudflare-service from tunnel `origin_ip` (not sent by client).

### Local endpoints

- `GET /api/health` — returns current `HealthReport` JSON (on-demand)
- `GET /api/busy` — GPU busy boolean

## Model verification (`pkg/verify`)

Server-as-judge: the client measures locally; Inferoute decides `verified` / `failed` / etc.

### Catalog (`catalog.go`)

- `GET /api/models/approved-builds?service_type=<type>` — public aliases and HF metadata (no hashes)
- Cached in memory; refreshed each health cycle

### Verification flow

| Engine | Local measurement | Server call |
|--------|-------------------|-------------|
| **Ollama** | Digest + size from `/api/tags` | `POST /api/provider/verify-model` |
| **vLLM** | SHA256 of weight files under HF cache or `model_path` | `POST /api/provider/verify-model` |

`ApplyToModels` enriches each model before health push and display.

### Verify result cache (10 min TTL)

To avoid hammering `verify-model` (especially from the 3s console redraw), results are cached per alias:

- **TTL:** 10 minutes (`verifyResultTTL`)
- **Invalidate when:** Ollama digest/size changes, vLLM weight file stats change, approved catalog fingerprint changes, or TTL expires
- **Inference:** `CheckInference` uses the same verify path; cache invalidates on real model changes

vLLM also keeps a **weight fingerprint cache** (`measureWithCache`) to skip re-hashing unchanged files on disk.

### Inference gate

Every `POST /v1/chat/completions` and `POST /v1/completions`:

1. Parse `model` from body
2. `CheckInference` → must be `verification_status == verified`
3. Validate `X-Request-Id` HMAC via `POST /api/provider/validate_hmac`
4. Forward to local LLM

Unapproved or failed models are rejected before proxying.

## Model pricing (`pkg/pricing`)

**At startup:** discover models, fetch averages (`POST /api/model-pricing/get-prices`), register via `POST /api/provider/models` (per-token prices).

**Ongoing:** each health cycle registers models not yet in the local tracker (HTTP 400 if already exists → mark tracked).

Only models with `verification_status` allowing inference registration are registered (verified).

## Cloudflare tunnel (`pkg/cloudflare`)

1. `POST /api/cloudflare/tunnel/request` with `service_url` (local proxy URL) and provider API key
2. Platform returns `token` + `hostname`
3. Client runs `cloudflared tunnel run --token <token>`
4. Supervision: health check every **10s**, restart on exit with exponential backoff (max 30s delay)

Tunnel URL included in health reports as `cloudflare.url`.

## HTTP server (`pkg/server`)

### Routes

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/health` | Health snapshot |
| GET | `/api/busy` | GPU busy |
| POST | `/v1/chat/completions` | OpenAI-compatible chat |
| POST | `/v1/completions` | OpenAI-compatible completions |

### Console UI

`consoleUpdater` redraws every **3 seconds**. Model status is read from `healthReporter.GetDisplayedModels()` (last health-sync snapshot) — **not** re-verified on every redraw.

Displays: session info, tunnel URL, GPU block, model approval status, recent requests, errors.

### GPU busy (`pkg/gpu`)

- **Linux + NVIDIA:** `nvidia-smi`; busy if utilization > **20%**
- **macOS:** always not busy
- **No monitor:** not busy

## Logging (`pkg/logger`)

Zap structured logging; files under `logging.log_dir` (default `~/.local/state/inferoute/log`). Levels: debug, info, warn, error. Rotation via lumberjack settings in config.

## Cross-platform behavior

| Platform | GPU detail | Busy detection |
|----------|------------|----------------|
| Linux + NVIDIA | Full via nvidia-smi | Utilization threshold |
| macOS | Basic via system_profiler | Always false |
| No GPU monitor | Placeholder values in health | Always false |

Client continues operating without GPU data.

## Deployment

- Native binary via install scripts (`scripts/install.sh`, macOS/Windows variants)
- Docker (`Dockerfile`, `scripts/entrypoint.sh`)
- Requires `cloudflared` on PATH (install scripts install it)

## Related platform docs

Cluster country resolution, MaxMind GeoLite2, and `POST /api/cloudflare/tunnel/sync-location` are documented in **inferoute-node** `documentation/technical.md` (Cloudflare Service).
