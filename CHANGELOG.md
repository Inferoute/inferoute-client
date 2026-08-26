# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [1.1.7] - 2026-08-26

### Added

- **Windows:** balloon/toast on tray start so launching the `.exe` from Explorer is not silent.
- **Local status dashboard** at `GET /` (HTML) and `GET /api/status` (JSON) — same session, model, GPU, and recent-request stats as the terminal UI.

- **In-flight inference cap** (default 1) so a second request gets **503** instead of queueing in Ollama/vLLM. Fixes macOS (no GPU busy signal) and the NVIDIA race where utilization is still 0% at request start. Set `server.max_concurrent_inference` to raise the cap, or `0` for unlimited.

### Changed

- **Windows:** tray mode is the default. `inferoute-client` detaches from PowerShell so closing the window does not stop the client. Use `--console` for the terminal UI. `--tray` is still accepted.
- Tray **Open dashboard** opens the local status page (`http://127.0.0.1:<port>/`), not the Inferoute website.
