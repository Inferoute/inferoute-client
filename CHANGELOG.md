# Changelog

All notable changes to the Inferoute Client will be documented in this file.

## [Unreleased]

### Added

- Catalog serve flags (`tool_call_parser`, `max_model_len`, `rope_type`, `rope_base_context_len`) drive engine start argv: vLLM/Metal get tools + `--max-model-len` + YaRN `--hf-overrides` when set; FreeToken gets `--max-seq-len-override` from the same `max_model_len`. Setup persists the fields in `config.yaml` for auto-start.
- Health verification fails vLLM/FreeToken models when live engine context is below catalog `max_model_len` (or unreadable). FreeToken falls back to `/v1/cache/status` geometry when the model card omits `max_model_len`.
- Compatibility / setup scoring scales vLLM required memory with catalog `max_model_len` (KV vs 8k baseline) so long-context builds are not marked as fitting on mid-VRAM cards.

## [1.1.9] - 2026-10-11

### Fixed

- Windows FreeToken setup installs CUDA PyTorch from `download.pytorch.org/whl/cu130`. PyPI's Windows `torch` wheel is CPU-only, so `ft serve` previously died with `only 0 device(s) visible` after a successful `/v1/models` health check. Re-running setup repairs an existing CPU venv.
- Cluster Ctrl-C now kills the per-machine runners. Background jobs were ignoring SIGINT, so JarvisLab wait loops kept printing after the prompt.
- JarvisLab resume treats "No free GPUs" / non-zero `jl resume` as a retry, not success — GPU fallback (H200 etc.) actually runs.
- Cloudflare tunnel origin now uses `http://127.0.0.1:<port>` instead of `localhost`. On macOS `localhost` is `::1` first, the client listens on IPv4 only, and inference through the tunnel 502s (Linux/Windows were fine).
- Windows: `nvidia-smi` (and `cloudflared`) no longer flash a console window. The dashboard polls GPU status every few seconds; those child processes now start with `CREATE_NO_WINDOW`.
- A wrong or missing provider API key now fails startup with a clear message instead of a generic platform **500**.
- Windows FreeToken setup installs only the CLI wheels (uv + beta `engine-win_amd64.json`) into `%LOCALAPPDATA%\inferoute\venv-freetoken`. It no longer runs the Desktop NSIS installer. Detect and auto-start ignore Desktop's bundled `resources\ft.exe`, which is not a serving binary.
- Setup does not print **Engine is ready** until Ollama/vLLM/FreeToken lists at least one model. An empty `/v1/models` 200 (typical while weights download) kept the spinner going. If the engine process dies, the wizard fails instead of hanging.
- FreeToken readiness uses `GET /health` with `status: "ok"`. `/v1/models` 200s while weights load and chat 503s until then.
- Windows tray startup waits until `http://127.0.0.1:<port>/` responds before printing that the client is running. The parent used to return as soon as the detached process spawned.

### Changed

- Setup/compatibility model table is numbered, usable rows (`runs_well` / `fits` / `tight`) are green, and the reason column is gone.
- Documented provider floor is **32 GB** of system memory and **24 GB** NVIDIA VRAM (Linux/Windows), or **48 GB** unified memory (Mac).
- Empty/`your_api_key_here` `api_key` is rejected locally before contacting the platform.
- `scripts/build.ps1` / `scripts/build.bat` build `inferoute-client.exe` on Windows (same ldflags as `scripts/build.sh`).
- Linux/macOS `install.sh` no longer requires `PROVIDER_API_KEY` in the curl line. The wizard asks. Use `INFEROUTE_SKIP_SETUP=1` for the old env-only path.
- Windows FreeToken engine wheels are resolved from the `beta` GitHub release (`engine-win_amd64.json`), not `latest`.

### Added

- Setup shows a spinner while fetching the approved-model catalog, including the API URL it is calling.
- Setup wait spinners rotate random AI quotes underneath the status line.
- Setup waits up to 2 hours for first-run model download and load instead of failing at 10 minutes and continuing anyway.
- Setup/auto-start do not spawn a second engine if the LLM port is already bound (avoids `Address already in use` during HuggingFace download).
- `INFEROUTE_URL` / `setup --url` override the Inferoute API base (catalog + `provider.url`). Flag wins over env.
- `inferoute-client setup` walks through engine, model, and API key. Re-run anytime to update config. Install scripts launch it after placing the binary.
- Auto-start: if `auto_start` is set, the client starts Ollama / vLLM / vLLM Metal / FreeToken when `llm_url` is down, then leaves that process running.
- Windows wizard installs the FreeToken CLI into `%LOCALAPPDATA%\inferoute\venv-freetoken` (not FreeToken Desktop).
- `scripts/e2e-test/run-cluster.sh` brings up Linux, Windows, and Mac Mini as three Ollama providers (same model), holds until Y/Ctrl-C, then pauses Windows + JarvisLab. Mini stays up. Per-machine keys: `PROVIDER_API_KEY_{LINUX,WINDOWS,MAC}`.

