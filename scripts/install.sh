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

# Install NGROK if not already installed
if ! command -v ngrok &> /dev/null; then
    echo -e "${YELLOW}NGROK not found. Installing...${NC}"
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    # Download NGROK based on OS and architecture
    NGROK_URL="https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-${OS_TYPE}-${ARCH_TYPE}.zip"
    echo -e "${BLUE}Downloading NGROK from: ${NGROK_URL}${NC}"
    
    # Use a more user-friendly progress display
    echo -e "${BLUE}Downloading NGROK...${NC}"
    if curl -#Lo ngrok.zip "$NGROK_URL"; then
        echo -e "${GREEN}Download complete!${NC}"
        # Extract NGROK
        echo -e "${BLUE}Extracting NGROK...${NC}"
        unzip -q -o ngrok.zip
        
        # Move NGROK to /usr/local/bin or ~/bin if not root
        if [ "$EUID" -eq 0 ]; then
            mv ngrok /usr/local/bin/
        else
            mkdir -p $HOME/bin
            mv ngrok $HOME/bin/
            
            # Add to PATH if not already there
            if [[ ":$PATH:" != *":$HOME/bin:"* ]]; then
                echo -e "${YELLOW}Adding $HOME/bin to PATH${NC}"
                echo 'export PATH="$HOME/bin:$PATH"' >> $HOME/.bashrc
                echo 'export PATH="$HOME/bin:$PATH"' >> $HOME/.zshrc 2>/dev/null || true
                export PATH="$HOME/bin:$PATH"
            fi
        fi
        
        echo -e "${GREEN}NGROK installed successfully.${NC}"
    else
        echo -e "${RED}Failed to download NGROK. Please install it manually.${NC}"
        echo -e "Visit https://ngrok.com/download for installation instructions."
        exit 1
    fi
    
    # Clean up
    cd - > /dev/null
    rm -rf $TEMP_DIR
else
    echo -e "${GREEN}NGROK is already installed.${NC}"
fi

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

# Now handle config.yaml setup
echo -e "\n${BLUE}=== Configuration Setup ===${NC}"

# Download config.yaml.example first
echo -e "${BLUE}Downloading config.yaml.example...${NC}"
curl -fsSL -o "$CONFIG_DIR/config.yaml" https://raw.githubusercontent.com/Inferoute/inferoute-client/main/config.yaml.example
echo -e "${GREEN}Configuration template downloaded.${NC}"

# Get configuration values from environment variables
NGROK_AUTHTOKEN=$(check_env_var "NGROK_AUTHTOKEN" "$NGROK_AUTHTOKEN" "")
PROVIDER_API_KEY=$(check_env_var "PROVIDER_API_KEY" "$PROVIDER_API_KEY" "")
PROVIDER_TYPE=$(check_env_var "PROVIDER_TYPE" "$PROVIDER_TYPE" "ollama")
LLM_URL=$(check_env_var "LLM_URL" "$LLM_URL" "http://localhost:11434")
SERVER_PORT=$(check_env_var "SERVER_PORT" "$SERVER_PORT" "8080")
NGROK_PORT=$(check_env_var "NGROK_PORT" "$NGROK_PORT" "4040")

# Configure NGROK authtoken
echo -e "${BLUE}Configuring NGROK authtoken...${NC}"
ngrok config add-authtoken "$NGROK_AUTHTOKEN" || {
    echo -e "${RED}Failed to configure NGROK authtoken${NC}"
    exit 1
}

# Check if NGROK is already running
if pgrep -f "ngrok http" > /dev/null; then
    echo -e "${GREEN}NGROK is already running.${NC}"
else
    echo -e "${BLUE}Starting NGROK...${NC}"
    # Kill any existing NGROK processes that might be stuck
    pkill -f ngrok || true
    sleep 2

    # Start NGROK with the configured port
    ngrok http $SERVER_PORT --log=stdout --host-header="localhost:$SERVER_PORT" > "$LOG_DIR/ngrok.log" 2>&1 &
    NGROK_PID=$!

    # Wait a moment to ensure NGROK starts
    sleep 5

    # Check if NGROK is running
    if ! ps -p $NGROK_PID > /dev/null; then
        echo -e "${RED}Error: NGROK failed to start${NC}"
        echo -e "${YELLOW}=== NGROK Log Contents ===${NC}"
        if [ -f "$LOG_DIR/ngrok.log" ]; then
            cat "$LOG_DIR/ngrok.log"
        else
            echo "Log file not found at $LOG_DIR/ngrok.log"
        fi
        echo -e "${YELLOW}=== End of NGROK Log ===${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}NGROK started successfully.${NC}"
    echo -e "${BLUE}NGROK admin interface available at: ${GREEN}http://localhost:4040${NC}"
fi

# Update configuration values
echo -e "${BLUE}Updating configuration values...${NC}"
if [ "$(uname)" = "Darwin" ]; then
    # macOS version
    sed -i '' "s|port: .*|port: $SERVER_PORT|" "$CONFIG_DIR/config.yaml"
    sed -i '' "s|api_key: .*|api_key: \"$PROVIDER_API_KEY\"|" "$CONFIG_DIR/config.yaml"
    sed -i '' "s|type: .*|type: \"$PROVIDER_TYPE\"|" "$CONFIG_DIR/config.yaml"
    sed -i '' "s|llm_url: .*|llm_url: \"$LLM_URL\"|" "$CONFIG_DIR/config.yaml"
    sed -i '' "s|authtoken: .*|authtoken: \"$NGROK_AUTHTOKEN\"|" "$CONFIG_DIR/config.yaml"
    sed -i '' "/ngrok:/,/port:/ s|port: .*|port: $NGROK_PORT|" "$CONFIG_DIR/config.yaml"
else
    # Linux version
    sed -i "s|port: .*|port: $SERVER_PORT|" "$CONFIG_DIR/config.yaml"
    sed -i "s|api_key: .*|api_key: \"$PROVIDER_API_KEY\"|" "$CONFIG_DIR/config.yaml"
    sed -i "s|type: .*|type: \"$PROVIDER_TYPE\"|" "$CONFIG_DIR/config.yaml"
    sed -i "s|llm_url: .*|llm_url: \"$LLM_URL\"|" "$CONFIG_DIR/config.yaml"
    sed -i "s|authtoken: .*|authtoken: \"$NGROK_AUTHTOKEN\"|" "$CONFIG_DIR/config.yaml"
    sed -i "/ngrok:/,/port:/ s|port: .*|port: $NGROK_PORT|" "$CONFIG_DIR/config.yaml"
fi

echo -e "${GREEN}Configuration file updated successfully.${NC}"

echo -e "\n${GREEN}Installation complete!${NC}"
echo -e "\n${BLUE}NGROK is running:${NC}"
echo -e "Admin interface: ${GREEN}http://localhost:4040${NC}"
echo -e "Logs: $LOG_DIR/ngrok.log"

echo -e "\n${BLUE}NGROK manual control:${NC}"
echo -e "Start: ${YELLOW}ngrok http $SERVER_PORT --log=stdout --host-header=\"localhost:$SERVER_PORT\" > $LOG_DIR/ngrok.log 2>&1 &${NC}"
echo -e "Stop:  ${YELLOW}pkill -f ngrok${NC}"

echo -e "\n${BLUE}INFEROUTE Files:${NC}"
echo -e "Config file: $CONFIG_DIR/config.yaml"
echo -e "Log directory: $LOG_DIR"

echo -e "\n${BLUE}INFEROUTE Start Command (Defaults to $CONFIG_DIR/config.yaml ):${NC}"
echo -e "${YELLOW}inferoute-client${NC}"
echo -e "Start with specific config:  ${YELLOW}inferoute-client --config $CONFIG_DIR/config.yaml${NC}"


