# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [Unreleased]

### Fixed

- Cloudflare tunnel origin now uses `http://127.0.0.1:<port>` instead of `localhost`. On macOS `localhost` is `::1` first, the client listens on IPv4 only, and inference through the tunnel 502s (Linux/Windows were fine).
- Windows: `nvidia-smi` (and `cloudflared`) no longer flash a console window. The dashboard polls GPU status every few seconds; those child processes now start with `CREATE_NO_WINDOW`.
- A wrong or missing provider API key now fails startup with a clear message instead of a generic platform **500**.

### Changed

- Empty/`your_api_key_here` `api_key` is rejected locally before contacting the platform.
- `scripts/build.ps1` / `scripts/build.bat` build `inferoute-client.exe` on Windows (same ldflags as `scripts/build.sh`).

## [1.1.8] - 2026-09-30

### Security

- Default listen address is **`127.0.0.1`** (was `0.0.0.0`). The local dashboard and `/api/*` stay off the LAN unless you set `server.host` yourself. Docker examples no longer publish port 8080 — the Cloudflare tunnel does not need it.
- Platform tunnel ingress forwards only **`POST /v1/chat/completions`** and **`POST /v1/completions`**. Operator routes (`/`, `/api/health`, `/api/busy`, `/api/status`) are loopback-only.
- Inference authenticates **before** the GPU busy check. Missing, oversized (`>256` bytes), or invalid `X-Request-Id` returns **401** without running `nvidia-smi` or revealing whether the GPU is busy (`503` vs `401`).
- `nvidia-smi` results are cached for **1s** and queries are serialized, so a request flood cannot stampede the NVIDIA driver.

### Changed

- Docker: to open the host dashboard, set `server.host: 0.0.0.0` and publish `-p 127.0.0.1:8080:8080`.

