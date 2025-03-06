# Changelog

All notable changes to the Inferoute Client will be documented in this file.


## [1.0.5] - 2025-03-03

### Added

- Added log echo for NGROK when it fails to start. Happens when using the same AUTH key twice. NGROK only allows AUTH key to be used for single NGROK session
- Updated install.sh with better logging output.
- Created build.sh script for testing
- Added model pricing - First time client launches it will create models and add model pricing for each model based on averages across all providers. Users can still manually update on website.
- Client now automatically picks up NGROK URL
- NGROK port is now configurable

### Fixed

- minor code fixes and refactoring
