# Changelog

## [1.1.10] - 2026-10-10

### Added

- `inferoute-client update` downloads the build the node advertises. You restart. Docker does not self-update.
- Startup and the running console say when an update is available or required.
- `POST /v1/embeddings` goes through the same HMAC proxy as chat.
- Catalog serve flags set engine start args. `engines` and `kv_cache_bytes_per_token` drive setup scoring. FreeToken is opt-in.
- Windows floor: 32 GB RAM plus a 64 GB page file, or 64 GB RAM, and 24 GB NVIDIA VRAM.

### Fixed

- FreeToken picks MoE placement per model: `fused` on the GPU, or `auto` when VRAM is tight.
- vLLM verification hashes nested files and skips `onnx/`, `original/`, `metal/`, and `mlx/`.
- The context gate reads live engine context instead of rejecting a bare model.

## [1.1.9] - 2026-10-11

### Fixed

- Windows FreeToken setup installs CUDA PyTorch, not the CPU wheel, and only the CLI (not Desktop).
- Tunnel origin is `127.0.0.1`. macOS `localhost` was IPv6 and inference 502'd.
- A bad API key fails at startup. Setup waits until the engine actually lists a model.

### Added

- `inferoute-client setup` asks for engine, model, and API key. Install scripts launch it.
- Auto-start brings the engine up when `llm_url` is down.
