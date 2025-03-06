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

# Build the binary with version information
go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" -o inferoute-client ./cmd

echo "Build complete: $(pwd)/inferoute-client"

# Check if config exists, if not copy example
if [ ! -f config.yaml ]; then
    if [ -f config.yaml.example ]; then
        echo "No config.yaml found, copying from example..."
        cp config.yaml.example config.yaml
        echo "Created config.yaml - please edit with your settings"
    fi
fi 