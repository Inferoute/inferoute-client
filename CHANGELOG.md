# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [Unreleased]

### Fixed

- Cluster Ctrl-C now kills the per-machine runners. Background jobs were ignoring SIGINT, so JarvisLab wait loops kept printing after the prompt.
- JarvisLab resume treats "No free GPUs" / non-zero `jl resume` as a retry, not success — GPU fallback (H200 etc.) actually runs.
- Cloudflare tunnel origin now uses `http://127.0.0.1:<port>` instead of `localhost`. On macOS `localhost` is `::1` first, the client listens on IPv4 only, and inference through the tunnel 502s (Linux/Windows were fine).
- Windows: `nvidia-smi` (and `cloudflared`) no longer flash a console window. The dashboard polls GPU status every few seconds; those child processes now start with `CREATE_NO_WINDOW`.
- A wrong or missing provider API key now fails startup with a clear message instead of a generic platform **500**.

### Changed

- Empty/`your_api_key_here` `api_key` is rejected locally before contacting the platform.
- `scripts/build.ps1` / `scripts/build.bat` build `inferoute-client.exe` on Windows (same ldflags as `scripts/build.sh`).
- Linux/macOS `install.sh` no longer requires `PROVIDER_API_KEY` in the curl line. The wizard asks. Use `INFEROUTE_SKIP_SETUP=1` for the old env-only path.

### Added

- `inferoute-client setup` walks through engine, model, and API key. Re-run anytime to update config. Install scripts launch it after placing the binary.
- Auto-start: if `auto_start` is set, the client starts Ollama / vLLM / vLLM Metal / FreeToken when `llm_url` is down, then leaves that process running.
- Windows wizard can silently install FreeToken (`FreeToken-Setup-win-x64.exe /S`) and locate `ft.exe`.
- `scripts/e2e-test/run-cluster.sh` brings up Linux, Windows, and Mac Mini as three Ollama providers (same model), holds until Y/Ctrl-C, then pauses Windows + JarvisLab. Mini stays up. Per-machine keys: `PROVIDER_API_KEY_{LINUX,WINDOWS,MAC}`.

## [1.1.8] - 2026-09-30

### Security

- Default listen address is **`127.0.0.1`** (was `0.0.0.0`). The local dashboard and `/api/*` stay off the LAN unless you set `server.host` yourself. Docker examples no longer publish port 8080 — the Cloudflare tunnel does not need it.
- Platform tunnel ingress forwards only **`POST /v1/chat/completions`** and **`POST /v1/completions`**. Operator routes (`/`, `/api/health`, `/api/busy`, `/api/status`) are loopback-only.
- Inference authenticates **before** the GPU busy check. Missing, oversized (`>256` bytes), or invalid `X-Request-Id` returns **401** without running `nvidia-smi` or revealing whether the GPU is busy (`503` vs `401`).
- `nvidia-smi` results are cached for **1s** and queries are serialized, so a request flood cannot stampede the NVIDIA driver.

### Changed

- Docker: to open the host dashboard, set `server.host: 0.0.0.0` and publish `-p 127.0.0.1:8080:8080`.

