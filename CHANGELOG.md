# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [1.0.1] - 2025-03-03

### Added
- NGROK now automatically starts
- Automatic installation of jq dependency if not present
- Cross-platform support for Linux and macOS (amd64 and arm64)
- Automatic configuration file management in ~/.config/inferoute
- Structured logging in ~/.local/state/inferoute/log
- Environment variable configuration support:
  - NGROK_AUTHTOKEN
  - PROVIDER_API_KEY
  - PROVIDER_TYPE
  - OLLAMA_URL
  - SERVER_PORT

### Changed
- Moved configuration to XDG standard directories
- Improved NGROK startup reliability with health checks
- Enhanced error handling and user feedback

### Fixed
- Fixed bug where Docker container was not auto starting NGROK
- Improved NGROK process management to prevent orphaned processes
- Better handling of existing NGROK installations

