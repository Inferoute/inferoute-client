#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check required environment variables
check_env_var() {
    local var_name="$1"
    local var_value="$2"
    local default_value="$3"
    
    if [ -z "$var_value" ] && [ -z "$default_value" ]; then
        echo -e "${RED}Error: Required environment variable $var_name is not set${NC}"
        echo "Please run the script with required environment variables:"
        echo "curl ... | NGROK_AUTHTOKEN=\"your-token\" PROVIDER_API_KEY=\"your-key\" bash"
        exit 1
    fi
    
    echo "${var_value:-$default_value}"
}

# Create config directory
CONFIG_DIR="$HOME/.config/inferoute"
LOG_DIR="$HOME/.local/state/inferoute/log"
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"
echo -e "${BLUE}Creating config directory: $CONFIG_DIR${NC}"
echo -e "${BLUE}Creating log directory: $LOG_DIR${NC}"

# Detect if script is being piped to sh/bash
if [ -z "$BASH_SOURCE" ] || [ "$BASH_SOURCE" = "$0" ]; then
    # Script is being run directly (not piped)
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
else
    # Script is being piped to sh/bash
    SCRIPT_DIR="/tmp"
    cd "$SCRIPT_DIR"
fi

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

echo -e "${BLUE}=== Inferoute Client Installation Script ===${NC}"
echo -e "${BLUE}Detected OS: ${OS_TYPE}, Architecture: ${ARCH} (${ARCH_TYPE})${NC}"

# Change to installation directory
cd "$SCRIPT_DIR"

# Function to get version from binary
get_binary_version() {
    local binary_path="/usr/local/bin/inferoute-client"
    if [ -f "$binary_path" ]; then
        "$binary_path" --version | head -n1 | cut -d' ' -f2
    else
        echo "0.0.0"
    fi
}

# Function to get latest version from GitHub
get_latest_version() {
    local repo="$1"
    local latest_version
    latest_version=$(curl -s "https://api.github.com/repos/${repo}/releases/latest" | grep -o '"tag_name": "[^"]*"' | sed 's/"tag_name": "//;s/^v//;s/"$//')
    if [ -z "$latest_version" ]; then
        echo "0.0.0"
    else
        echo "$latest_version"
    fi
}

# Function to compare versions
version_gt() {
    test "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1"
}

# Download and install inferoute-client binary
install_binary() {
    echo -e "${BLUE}Downloading inferoute-client binary...${NC}"
    
    # Set GitHub repository and latest release info
    GITHUB_REPO="inferoute/inferoute-client"
    BINARY_NAME="inferoute-client-${OS_TYPE}-${ARCH_TYPE}"
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}.zip"
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    echo -e "${BLUE}Downloading from: ${DOWNLOAD_URL}${NC}"
    
    # Use a more user-friendly progress display
    if curl -#Lo "${BINARY_NAME}.zip" "$DOWNLOAD_URL"; then
        echo -e "${GREEN}Download complete!${NC}"
        SUMS_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/checksums.txt"
        if curl -fsSL -o checksums.txt "$SUMS_URL"; then
            expected=$(awk -v f="${BINARY_NAME}.zip" '$2 == f { print $1; exit }' checksums.txt)
            if [ -z "$expected" ]; then
                echo -e "${RED}checksums.txt has no entry for ${BINARY_NAME}.zip${NC}"
                exit 1
            fi
            if command -v sha256sum >/dev/null 2>&1; then
                actual=$(sha256sum "${BINARY_NAME}.zip" | awk '{print $1}')
            else
                actual=$(shasum -a 256 "${BINARY_NAME}.zip" | awk '{print $1}')
            fi
            if [ "$actual" != "$expected" ]; then
                echo -e "${RED}Checksum mismatch for ${BINARY_NAME}.zip${NC}"
                exit 1
            fi
            echo -e "${GREEN}Checksum verified.${NC}"
        else
            echo -e "${YELLOW}No checksums.txt on this release; continuing without verification.${NC}"
        fi
        # Extract binary
        echo -e "${BLUE}Extracting binary...${NC}"
        unzip -q -o "${BINARY_NAME}.zip"
        
        # Move binary to /usr/local/bin
        sudo mv $BINARY_NAME /usr/local/bin/inferoute-client
        sudo chmod +x /usr/local/bin/inferoute-client

    else
        echo -e "${RED}Failed to download inferoute-client binary${NC}"
        echo -e "${YELLOW}Please check if the release exists at: https://github.com/${GITHUB_REPO}/releases${NC}"
        exit 1
    fi
    
    # Clean up
    cd - > /dev/null
    rm -rf $TEMP_DIR
}

# Check and install inferoute-client binary
GITHUB_REPO="inferoute/inferoute-client"
if [ -f "/usr/local/bin/inferoute-client" ]; then
    echo -e "${BLUE}Checking for updates...${NC}"
    CURRENT_VERSION=$(get_binary_version)
    LATEST_VERSION=$(get_latest_version "$GITHUB_REPO")
    
    echo -e "Current version: ${YELLOW}${CURRENT_VERSION}${NC}"
    echo -e "Latest version:  ${YELLOW}${LATEST_VERSION}${NC}"
    
    if version_gt "$LATEST_VERSION" "$CURRENT_VERSION"; then
        echo -e "${YELLOW}New version available. Updating...${NC}"
        install_binary
    else
        echo -e "${GREEN}You have the latest version.${NC}"
    fi
else
    echo -e "${YELLOW}No existing installation found. Installing...${NC}"
    install_binary
fi

# Configuration: interactive wizard, or env-based when INFEROUTE_SKIP_SETUP=1
echo -e "\n${BLUE}=== Configuration Setup ===${NC}"

if [ "${INFEROUTE_SKIP_SETUP:-0}" = "1" ]; then
    echo -e "${YELLOW}INFEROUTE_SKIP_SETUP=1 — writing config from environment (no wizard).${NC}"
    curl -fsSL -o "$CONFIG_DIR/config.yaml" https://raw.githubusercontent.com/Inferoute/inferoute-client/main/config.yaml.example
    PROVIDER_API_KEY="${PROVIDER_API_KEY:-}"
    PROVIDER_TYPE="${PROVIDER_TYPE:-vllm}"
    DEFAULT_LLM_URL="http://127.0.0.1:8000"
    LLM_URL=$(check_env_var "LLM_URL" "$LLM_URL" "$DEFAULT_LLM_URL")
    SERVER_PORT=$(check_env_var "SERVER_PORT" "$SERVER_PORT" "8080")
    if [ -z "$PROVIDER_API_KEY" ]; then
        echo -e "${RED}Error: PROVIDER_API_KEY is required when INFEROUTE_SKIP_SETUP=1${NC}"
        exit 1
    fi
    if [ "$(uname)" = "Darwin" ]; then
        sed -i '' "s|port: .*|port: $SERVER_PORT|" "$CONFIG_DIR/config.yaml"
        sed -i '' "s|api_key: .*|api_key: \"$PROVIDER_API_KEY\"|" "$CONFIG_DIR/config.yaml"
        sed -i '' "s|provider_type: .*|provider_type: \"$PROVIDER_TYPE\"|" "$CONFIG_DIR/config.yaml"
        sed -i '' "s|llm_url: .*|llm_url: \"$LLM_URL\"|" "$CONFIG_DIR/config.yaml"
    else
        sed -i "s|port: .*|port: $SERVER_PORT|" "$CONFIG_DIR/config.yaml"
        sed -i "s|api_key: .*|api_key: \"$PROVIDER_API_KEY\"|" "$CONFIG_DIR/config.yaml"
        sed -i "s|provider_type: .*|provider_type: \"$PROVIDER_TYPE\"|" "$CONFIG_DIR/config.yaml"
        sed -i "s|llm_url: .*|llm_url: \"$LLM_URL\"|" "$CONFIG_DIR/config.yaml"
    fi
    echo -e "${GREEN}Configuration file written.${NC}"
else
    SETUP_ARGS=(setup --config "$CONFIG_DIR/config.yaml")
    if [ -n "$PROVIDER_API_KEY" ]; then
        SETUP_ARGS+=(--api-key "$PROVIDER_API_KEY")
    fi
    if [ -n "$PROVIDER_TYPE" ]; then
        SETUP_ARGS+=(--engine "$PROVIDER_TYPE")
    fi
    if [ -r /dev/tty ]; then
        echo -e "${BLUE}Starting setup wizard...${NC}"
        inferoute-client "${SETUP_ARGS[@]}" </dev/tty
    else
        echo -e "${YELLOW}No TTY detected. Run the wizard next:${NC}"
        echo -e "  ${YELLOW}inferoute-client setup${NC}"
    fi
fi

echo -e "\n${GREEN}Installation complete!${NC}"
echo -e "\n${BLUE}INFEROUTE Files:${NC}"
echo -e "Config file: $CONFIG_DIR/config.yaml"
echo -e "Log directory: $LOG_DIR"

echo -e "\n${BLUE}Start the client:${NC}"
echo -e "  ${YELLOW}inferoute-client${NC}"
echo -e "Re-run the wizard anytime:  ${YELLOW}inferoute-client setup${NC}"


