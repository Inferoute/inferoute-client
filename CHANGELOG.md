# Changelog

## [1.2.0] - 2026-10-11

### Removed

- Ollama. Setup, install scripts, and the client only run vLLM, vLLM Metal, or FreeToken.

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

