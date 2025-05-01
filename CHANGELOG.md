# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [1.0.7.2] - 2025-04-30

### Added

- Changed config to automatically set default LLM_URL based on provider type:
  - Uses http://127.0.0.1:8000 for vllm
  - Uses http://localhost:11434 for ollama (default)


### Fixed

