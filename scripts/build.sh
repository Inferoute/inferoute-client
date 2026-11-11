#!/bin/bash

# Exit on error
set -e

# Get the directory of the script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR/.."

# Build info
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE=$(date -u '+%Y-%m-%d-%H:%M UTC')

echo "Building inferoute-client..."
echo "Version: $VERSION"
echo "Commit:  $COMMIT"
echo "Date:    $DATE"

# On macOS the GPU power/temperature reader is C (cgo), which needs Apple's
# compiler. Without it Go silently drops those readings, so fail loudly.
if [ "$(uname -s)" = "Darwin" ]; then
    if ! xcrun --find clang >/dev/null 2>&1; then
        echo "Error: Xcode Command Line Tools not found. Install them with: xcode-select --install" >&2
        exit 1
    fi
    export CGO_ENABLED=1
    echo "macOS:   building with GPU power/temperature support (cgo)"
fi

# Build the binary with version information
go build -ldflags "-X 'github.com/sentnl/inferoute-node/inferoute-client/pkg/version.Version=$VERSION' -X 'github.com/sentnl/inferoute-node/inferoute-client/pkg/version.Commit=$COMMIT' -X 'github.com/sentnl/inferoute-node/inferoute-client/pkg/version.Date=$DATE'" -o inferoute-client ./cmd

echo "Build complete: $(pwd)/inferoute-client"

# Check if config exists, if not copy example
if [ ! -f config.yaml ]; then
    if [ -f config.yaml.example ]; then
        echo "No config.yaml found, copying from example..."
        cp config.yaml.example config.yaml
        echo "Created config.yaml - please edit with your settings"
    fi
fi 