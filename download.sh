#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Inferoute Client Download Script ===${NC}"

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

case "${OS}" in
    Linux*)     
        OS_TYPE=linux
        if [ "$ARCH" = "x86_64" ]; then
            ARCH_TYPE=amd64
        elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
            ARCH_TYPE=arm64
        else
            echo -e "${RED}Unsupported architecture: ${ARCH}${NC}"
            exit 1
        fi
        ;;
    Darwin*)    
        OS_TYPE=darwin
        if [ "$ARCH" = "x86_64" ]; then
            ARCH_TYPE=amd64
        elif [ "$ARCH" = "arm64" ]; then
            ARCH_TYPE=arm64
        else
            echo -e "${RED}Unsupported architecture: ${ARCH}${NC}"
            exit 1
        fi
        ;;
    *)          
        echo -e "${RED}Unsupported OS: ${OS}${NC}"
        exit 1
        ;;
esac

echo -e "${BLUE}Detected OS: ${OS_TYPE}, Architecture: ${ARCH} (${ARCH_TYPE})${NC}"

# Set GitHub repository and latest release info
GITHUB_REPO="sentnl/inferoute-client"
BINARY_NAME="inferoute-client-${OS_TYPE}-${ARCH_TYPE}"
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}.zip"

echo -e "${BLUE}Downloading from: ${DOWNLOAD_URL}${NC}"

# Create temp directory
TEMP_DIR=$(mktemp -d)
cd $TEMP_DIR

if curl -Lo "${BINARY_NAME}.zip" "$DOWNLOAD_URL"; then
    # Extract binary
    unzip -o "${BINARY_NAME}.zip"
    
    # Move binary to current directory
    mv $BINARY_NAME ../inferoute-client
    chmod +x ../inferoute-client
    
    echo -e "${GREEN}inferoute-client downloaded successfully.${NC}"
else
    echo -e "${RED}Failed to download inferoute-client binary.${NC}"
    echo -e "${YELLOW}Please check if the release exists at: https://github.com/${GITHUB_REPO}/releases${NC}"
    exit 1
fi

# Clean up
cd - > /dev/null
rm -rf $TEMP_DIR

echo -e "${GREEN}Download complete!${NC}"
echo -e "${BLUE}You can now run the client with:${NC}"
echo -e "  ./inferoute-client -config config.yaml"
echo -e "${YELLOW}Note: Make sure you have a config.yaml file in the current directory.${NC}"
echo -e "You can download the example config with:"
echo -e "  curl -O https://raw.githubusercontent.com/${GITHUB_REPO}/main/config.yaml.example" 