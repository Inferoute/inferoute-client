#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create installation directory in user's home folder
INSTALL_DIR="$HOME/inferoute-client"
mkdir -p "$INSTALL_DIR"
echo -e "${BLUE}Creating installation directory: $INSTALL_DIR${NC}"

# Detect if script is being piped to sh/bash
if [ -z "$BASH_SOURCE" ] || [ "$BASH_SOURCE" = "$0" ]; then
    # Script is being run directly (not piped)
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
else
    # Script is being piped to sh/bash, use the installation directory
    SCRIPT_DIR="$INSTALL_DIR"
    cd "$SCRIPT_DIR"
    echo -e "${BLUE}Working in installation directory: $SCRIPT_DIR${NC}"
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
cd "$INSTALL_DIR"

# Check if jq is installed (needed for parsing NGROK API response)
if ! command -v jq &> /dev/null; then
    echo -e "${YELLOW}jq is not installed. Installing...${NC}"
    
    if [ "$OS_TYPE" = "linux" ]; then
        if command -v apt-get &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y jq
        elif command -v yum &> /dev/null; then
            sudo yum install -y jq
        elif command -v dnf &> /dev/null; then
            sudo dnf install -y jq
        else
            echo -e "${RED}Could not install jq. Please install it manually.${NC}"
            exit 1
        fi
    elif [ "$OS_TYPE" = "darwin" ]; then
        if command -v brew &> /dev/null; then
            brew install jq
        else
            echo -e "${RED}Homebrew not found. Please install Homebrew or jq manually.${NC}"
            echo -e "Visit https://brew.sh/ for Homebrew installation instructions."
            exit 1
        fi
    fi
    
    echo -e "${GREEN}jq installed successfully.${NC}"
else
    echo -e "${GREEN}jq is already installed.${NC}"
fi

# Install NGROK if not already installed
if ! command -v ngrok &> /dev/null; then
    echo -e "${YELLOW}NGROK not found. Installing...${NC}"
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    # Download NGROK based on OS and architecture
    NGROK_URL="https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-${OS_TYPE}-${ARCH_TYPE}.zip"
    echo -e "${BLUE}Downloading NGROK from: ${NGROK_URL}${NC}"
    
    if curl -Lo ngrok.zip "$NGROK_URL"; then
        # Extract NGROK
        unzip -o ngrok.zip
        
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

# Download inferoute-client binary
if [ ! -f "/usr/local/bin/inferoute-client" ]; then
    echo -e "${BLUE}Downloading inferoute-client binary...${NC}"
    
    # Set GitHub repository and latest release info
    GITHUB_REPO="inferoute/inferoute-client"
    BINARY_NAME="inferoute-client-${OS_TYPE}-${ARCH_TYPE}"
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}.zip"
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    echo -e "${BLUE}Downloading from: ${DOWNLOAD_URL}${NC}"
    
    if curl -Lo "${BINARY_NAME}.zip" "$DOWNLOAD_URL"; then
        # Extract binary
        unzip -o "${BINARY_NAME}.zip"
        
        # Move binary to /usr/local/bin
        sudo mv $BINARY_NAME /usr/local/bin/inferoute-client
        sudo chmod +x /usr/local/bin/inferoute-client
        
        echo -e "${GREEN}inferoute-client downloaded successfully.${NC}"
    else
        echo -e "${RED}Failed to download inferoute-client binary.${NC}"
        echo -e "${YELLOW}Please check if the release exists at: https://github.com/${GITHUB_REPO}/releases${NC}"
        exit 1
    fi
    
    # Clean up
    cd - > /dev/null
    rm -rf $TEMP_DIR
else
    echo -e "${GREEN}inferoute-client binary already exists.${NC}"
fi

# Now handle config.yaml setup
echo -e "\n${BLUE}=== Configuration Setup ===${NC}"

# Download config.yaml.example first
echo -e "${BLUE}Downloading config.yaml.example...${NC}"
curl -fsSL -o "$INSTALL_DIR/config.yaml.example" https://raw.githubusercontent.com/Inferoute/inferoute-client/main/config.yaml.example

# Check if config.yaml already exists
CONFIG_EXISTS=false
if [ -f "$INSTALL_DIR/config.yaml" ]; then
    echo -e "${YELLOW}Existing config.yaml found.${NC}"
    read -p "Do you want to keep the existing configuration? (Y/n): " KEEP_CONFIG
    if [[ $KEEP_CONFIG =~ ^[Nn]$ ]]; then
        echo -e "${YELLOW}Creating new configuration...${NC}"
        mv "$INSTALL_DIR/config.yaml" "$INSTALL_DIR/config.yaml.backup"
        echo -e "${YELLOW}Existing config backed up to: config.yaml.backup${NC}"
    else
        CONFIG_EXISTS=true
        echo -e "${GREEN}Keeping existing configuration.${NC}"
    fi
fi

if [ "$CONFIG_EXISTS" = false ]; then
    # Get NGROK authtoken
    echo -e "\n${BLUE}NGROK Configuration${NC}"
    read -p "Enter your NGROK authtoken: " NGROK_AUTHTOKEN
    while [ -z "$NGROK_AUTHTOKEN" ]; do
        echo -e "${RED}NGROK authtoken cannot be empty${NC}"
        read -p "Enter your NGROK authtoken: " NGROK_AUTHTOKEN
    done

    # Get Provider API key
    echo -e "\n${BLUE}Provider Configuration${NC}"
    read -p "Enter your Provider API key from inferoute.com: " PROVIDER_API_KEY
    while [ -z "$PROVIDER_API_KEY" ]; do
        echo -e "${RED}Provider API key cannot be empty${NC}"
        read -p "Enter your Provider API key: " PROVIDER_API_KEY
    done

    # Get Provider type with default
    read -p "Enter provider type [ollama]: " PROVIDER_TYPE
    PROVIDER_TYPE=${PROVIDER_TYPE:-ollama}

    # Get Ollama URL with default
    read -p "Enter Ollama URL [http://localhost:11434]: " OLLAMA_URL
    OLLAMA_URL=${OLLAMA_URL:-http://localhost:11434}

    # Get Server port with default
    read -p "Enter server port [8080]: " SERVER_PORT
    SERVER_PORT=${SERVER_PORT:-8080}

    # Create config.yaml with user inputs
    echo -e "${BLUE}Creating config.yaml with provided values...${NC}"
    cat > "$INSTALL_DIR/config.yaml" << EOF
server:
  port: $SERVER_PORT

ngrok:
  authtoken: "$NGROK_AUTHTOKEN"
  url: ""  # Will be updated automatically when NGROK starts

provider:
  type: "$PROVIDER_TYPE"
  api_key: "$PROVIDER_API_KEY"
  ollama_url: "$OLLAMA_URL"
EOF

    echo -e "${GREEN}Configuration file created successfully.${NC}"
else
    # If keeping existing config, still need to get the authtoken for NGROK setup
    NGROK_AUTHTOKEN=$(grep -A 5 "ngrok:" "$INSTALL_DIR/config.yaml" | grep "authtoken:" | awk -F'"' '{print $2}')
    SERVER_PORT=$(grep -A 5 "server:" "$INSTALL_DIR/config.yaml" | grep "port:" | awk '{print $2}')
    SERVER_PORT=${SERVER_PORT:-8080}
fi

# Configure NGROK authtoken
echo -e "${BLUE}Configuring NGROK authtoken...${NC}"
ngrok config add-authtoken "$NGROK_AUTHTOKEN" || {
    echo -e "${RED}Failed to configure NGROK authtoken${NC}"
    exit 1
}

# Check if NGROK is already running
NGROK_URL=""
if pgrep -f "ngrok http" > /dev/null; then
    echo -e "${YELLOW}NGROK is already running, getting existing URL...${NC}"
    # Try to get existing URL
    TUNNELS_DATA=$(curl -s http://localhost:4040/api/tunnels)
    if [ $? -eq 0 ]; then
        NGROK_URL=$(echo "$TUNNELS_DATA" | jq -r '.tunnels[0].public_url' 2>/dev/null)
        if [ "$NGROK_URL" != "null" ] && [ ! -z "$NGROK_URL" ]; then
            echo -e "${GREEN}Found existing NGROK URL: $NGROK_URL${NC}"
        fi
    fi
fi

# Start NGROK if not already running or if URL not found
if [ -z "$NGROK_URL" ]; then
    echo -e "${BLUE}Starting NGROK...${NC}"
    # Kill any existing NGROK processes that might be stuck
    pkill -f ngrok || true
    sleep 2

    # Start NGROK with the configured port
    ngrok http $SERVER_PORT --log=stdout --host-header="localhost:$SERVER_PORT" > "$INSTALL_DIR/ngrok.log" 2>&1 &
    NGROK_PID=$!

    # Wait for NGROK to start
    echo "Waiting for NGROK to initialize..."
    MAX_ATTEMPTS=30
    ATTEMPT=0

    while [ -z "$NGROK_URL" ] && [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        ATTEMPT=$((ATTEMPT+1))
        sleep 2

        # Check if NGROK is still running
        if ! ps -p $NGROK_PID > /dev/null; then
            echo -e "${RED}Error: NGROK failed to start!${NC}"
            echo "Check $INSTALL_DIR/ngrok.log for details"
            exit 1
        fi

        # Try to get URL
        TUNNELS_DATA=$(curl -s http://localhost:4040/api/tunnels)
        if [ $? -eq 0 ]; then
            NGROK_URL=$(echo "$TUNNELS_DATA" | jq -r '.tunnels[0].public_url' 2>/dev/null)
            if [ "$NGROK_URL" != "null" ] && [ ! -z "$NGROK_URL" ]; then
                echo -e "${GREEN}NGROK started successfully with URL: $NGROK_URL${NC}"
                break
            fi
        fi
        echo "Waiting for NGROK URL (attempt $ATTEMPT/$MAX_ATTEMPTS)..."
    done

    if [ -z "$NGROK_URL" ]; then
        echo -e "${RED}Failed to get NGROK URL after $MAX_ATTEMPTS attempts${NC}"
        echo "Check $INSTALL_DIR/ngrok.log for details"
        pkill -f ngrok || true
        exit 1
    fi
fi

# Update config.yaml with NGROK URL
echo -e "${BLUE}Updating config.yaml with NGROK URL...${NC}"
if [ "$(uname)" = "Darwin" ]; then
    # macOS requires different sed syntax
    sed -i '' "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_URL\"|" "$INSTALL_DIR/config.yaml"
else
    # Linux version
    sed -i "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_URL\"|" "$INSTALL_DIR/config.yaml"
fi

echo -e "\n${GREEN}Installation complete!${NC}"
echo -e "\n${BLUE}NGROK is running:${NC}"
echo -e "URL: ${GREEN}$NGROK_URL${NC}"
echo -e "Admin interface: ${GREEN}http://localhost:4040${NC}"
echo -e "Logs: $INSTALL_DIR/ngrok.log"

echo -e "\n${BLUE}To start inferoute-client:${NC}"
echo -e "${YELLOW}inferoute-client -config $INSTALL_DIR/config.yaml${NC}"

echo -e "\n${BLUE}Manual NGROK control:${NC}"
echo -e "Start: ${YELLOW}ngrok http $SERVER_PORT --log=stdout --host-header=\"localhost:$SERVER_PORT\" > $INSTALL_DIR/ngrok.log 2>&1 &${NC}"
echo -e "Stop:  ${YELLOW}pkill -f ngrok${NC}"

echo -e "\n${BLUE}Configuration:${NC}"
echo -e "Config file: $INSTALL_DIR/config.yaml"

