# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [1.1.2] - 2026-06-22

### Changed

- **vLLM weight discovery** — no required `model_path`. The client reads the model id from vLLM (`GET /v1/models`), looks up the approved `hf_revision`, and fingerprints weights under `~/.cache/huggingface/hub/models--Org--Name/snapshots/<revision>`.
- Optional `hf_hub_cache` if your HuggingFace cache is not in the default location.
- Optional `model_path` only for flat directories from `hf download --local-dir`.

## [1.1.1] - 2026-06-22

### Removed

- `provider.model_verification` config opt-out — integrity checks always run; there is no supported way to disable verification on the client.

## [1.1.0] - 2026-06-22

### Added

- **Model integrity verification (SNTNL-61)** — client fetches platform-approved model builds and verifies local models before registration, health reporting, and inference.
- **Ollama** — compares `/api/tags` digest and size against the approved build registry; no extra config required beyond `llm_url`.
- **vLLM** — fingerprints weight files on disk via `model_path` using the same manifest hashing as the platform bootstrap script (`safetensors_header` for `.safetensors`, full SHA-256 for other manifest files).
- Health reports now include `digest`, `weight_fingerprint`, `size_bytes`, and `verification_status` per model.
- Inference requests for unverified models return **403 Forbidden**.
- Config: `provider.model_path` (vLLM) for on-disk weight fingerprinting.

### Changed

- Only **verified** models are registered with the platform when verification is enabled.
- Ollama model listing preserves digest and size from `/api/tags` (previously discarded).

## [1.0.9] - 2025-04-30

### Added

- Added automated tunnel creation using cloudflare
- Security enhancements

### Fixed

- Cloudflare disconnecting after 30 seconds.

