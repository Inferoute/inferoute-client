#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Detect if script is being piped to sh/bash
if [ -z "$BASH_SOURCE" ] || [ "$BASH_SOURCE" = "$0" ]; then
    # Script is being run directly (not piped)
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
else
    # Script is being piped to sh/bash, create a temporary directory
    SCRIPT_DIR="$(mktemp -d)"
    cd "$SCRIPT_DIR"
    echo -e "${BLUE}Created temporary directory: $SCRIPT_DIR${NC}"
    
    # Download config.yaml.example if it doesn't exist
    if [ ! -f "config.yaml.example" ]; then
        echo -e "${BLUE}Downloading config.yaml.example...${NC}"
        curl -fsSL -o config.yaml.example https://raw.githubusercontent.com/sentnl/inferoute-client/main/config.yaml.example
        
        # Create config.yaml if it doesn't exist
        if [ ! -f "config.yaml" ]; then
            echo -e "${YELLOW}Creating config.yaml from example...${NC}"
            cp config.yaml.example config.yaml
            echo -e "${YELLOW}Please edit config.yaml to add your NGROK authtoken and other settings.${NC}"
            echo -e "${YELLOW}You can do this by running: nano config.yaml${NC}"
            echo -e "${YELLOW}Press Enter to continue after editing the file...${NC}"
            read -p ""
        fi
    fi
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

# Check if config.yaml exists
if [ ! -f "config.yaml" ]; then
    echo -e "${YELLOW}config.yaml not found.${NC}"
    
    # Check if config.yaml.example exists, download if not
    if [ ! -f "config.yaml.example" ]; then
        echo -e "${BLUE}Downloading config.yaml.example...${NC}"
        curl -fsSL -o config.yaml.example https://raw.githubusercontent.com/sentnl/inferoute-client/main/config.yaml.example
    fi
    
    # Create config.yaml from example
    echo -e "${YELLOW}Creating config.yaml from example...${NC}"
    cp config.yaml.example config.yaml
    echo -e "${YELLOW}Please edit config.yaml to add your NGROK authtoken and other settings.${NC}"
    echo -e "${YELLOW}You can do this by running: nano config.yaml${NC}"
    echo -e "${YELLOW}Press Enter to continue after editing the file...${NC}"
    read -p ""
fi

# Check if NGROK authtoken is in config.yaml
NGROK_AUTHTOKEN=$(grep -A 5 "ngrok:" config.yaml | grep "authtoken:" | awk -F'"' '{print $2}')
if [ -z "$NGROK_AUTHTOKEN" ]; then
    echo -e "${RED}Error: NGROK authtoken not found in config.yaml${NC}"
    echo -e "Please add 'authtoken: \"your_ngrok_authtoken_here\"' under the ngrok section in config.yaml"
    echo -e "You can do this by running: nano config.yaml"
    exit 1
fi

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

# Configure NGROK using the official method
echo -e "${BLUE}Configuring NGROK...${NC}"
ngrok config add-authtoken "$NGROK_AUTHTOKEN"
echo -e "${GREEN}NGROK configured successfully.${NC}"

# Download inferoute-client binary
if [ ! -f "./inferoute-client" ]; then
    echo -e "${BLUE}Downloading inferoute-client binary...${NC}"
    
    # Set GitHub repository and latest release info
    GITHUB_REPO="sentnl/inferoute-client"
    BINARY_NAME="inferoute-client-${OS_TYPE}-${ARCH_TYPE}"
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}.zip"
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    echo -e "${BLUE}Downloading from: ${DOWNLOAD_URL}${NC}"
    
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
else
    echo -e "${GREEN}inferoute-client binary already exists.${NC}"
fi

# Create run directory
mkdir -p run

# Create start script
cat > run/start.sh << 'EOF'
#!/bin/bash
set -e

# Get the directory of this script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd $DIR/..

# Get server port from config.yaml
SERVER_PORT=$(grep -A 5 "server:" config.yaml | grep "port:" | awk '{print $2}')
if [ -z "$SERVER_PORT" ]; then
    SERVER_PORT=8080
    echo "Server port not found in config.yaml, using default: $SERVER_PORT"
fi

# Start NGROK in background
echo "Starting NGROK tunnel..."
ngrok http $SERVER_PORT --log=stdout --host-header="localhost:$SERVER_PORT" > run/ngrok.log 2>&1 &
NGROK_PID=$!

# Save PID for later cleanup
echo $NGROK_PID > run/ngrok.pid

# Wait for NGROK to start
echo "Waiting for NGROK to start..."
sleep 5

# Get NGROK public URL
echo "Getting NGROK public URL..."
NGROK_PUBLIC_URL=""
MAX_ATTEMPTS=30
ATTEMPT=0

while [ -z "$NGROK_PUBLIC_URL" ] && [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    ATTEMPT=$((ATTEMPT+1))
    echo "Trying to get NGROK public URL (attempt $ATTEMPT/$MAX_ATTEMPTS)..."
    NGROK_PUBLIC_URL=$(curl -s http://localhost:4040/api/tunnels | jq -r '.tunnels[0].public_url')
    
    if [ "$NGROK_PUBLIC_URL" == "null" ] || [ -z "$NGROK_PUBLIC_URL" ]; then
        echo "NGROK not ready yet, waiting..."
        NGROK_PUBLIC_URL=""
        sleep 2
    fi
done

if [ -z "$NGROK_PUBLIC_URL" ]; then
    echo "Failed to get NGROK public URL after $MAX_ATTEMPTS attempts."
    echo "Check run/ngrok.log for details."
    echo "Stopping NGROK..."
    kill $NGROK_PID
    exit 1
fi

echo "NGROK public URL: $NGROK_PUBLIC_URL"

# Update config.yaml with NGROK URL
if [ "$(uname)" = "Darwin" ]; then
    # macOS requires different sed syntax
    sed -i '' "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_PUBLIC_URL\"|" config.yaml
else
    # Linux version
    sed -i "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_PUBLIC_URL\"|" config.yaml
fi

# Start inferoute-client
echo "Starting inferoute-client..."
./inferoute-client -config config.yaml
EOF

# Create stop script
cat > run/stop.sh << 'EOF'
#!/bin/bash

# Get the directory of this script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd $DIR

# Check if NGROK is running
if [ -f "ngrok.pid" ]; then
    NGROK_PID=$(cat ngrok.pid)
    if ps -p $NGROK_PID > /dev/null; then
        echo "Stopping NGROK (PID: $NGROK_PID)..."
        kill $NGROK_PID
    else
        echo "NGROK is not running."
    fi
    rm -f ngrok.pid
else
    echo "NGROK PID file not found."
fi

# Find and kill inferoute-client process
INFEROUTE_PID=$(pgrep -f "inferoute-client -config")
if [ ! -z "$INFEROUTE_PID" ]; then
    echo "Stopping inferoute-client (PID: $INFEROUTE_PID)..."
    kill $INFEROUTE_PID
else
    echo "inferoute-client is not running."
fi

echo "All processes stopped."
EOF

# Make scripts executable
chmod +x run/start.sh run/stop.sh

echo -e "${GREEN}Installation complete!${NC}"
echo -e "${BLUE}To start inferoute-client with NGROK:${NC}"
echo -e "  ./run/start.sh"
echo -e "${BLUE}To stop all services:${NC}"
echo -e "  ./run/stop.sh"
echo -e "${YELLOW}Note: NGROK admin interface will be available at http://localhost:4040${NC}" 