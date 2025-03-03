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
if [ ! -f "./inferoute-client" ]; then
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
        
        # Move binary to current directory
        mv $BINARY_NAME /usr/local/bin/inferoute-client
        chmod +x /usr/local/bin/inferoute-client
        
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

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get server port from config.yaml
SERVER_PORT=$(grep -A 5 "server:" config.yaml | grep "port:" | awk '{print $2}')
if [ -z "$SERVER_PORT" ]; then
    SERVER_PORT=8080
    echo "Server port not found in config.yaml, using default: $SERVER_PORT"
fi

# Configure NGROK authtoken from config.yaml
NGROK_AUTHTOKEN=$(grep -A 5 "ngrok:" config.yaml | grep "authtoken:" | awk -F'"' '{print $2}')
if [ -z "$NGROK_AUTHTOKEN" ]; then
    echo -e "${RED}Error: NGROK authtoken not found in config.yaml${NC}"
    echo -e "Please add 'authtoken: \"your_ngrok_authtoken_here\"' under the ngrok section in config.yaml"
    exit 1
fi

# Configure NGROK authtoken
echo -e "${BLUE}Configuring NGROK authtoken...${NC}"
ngrok config add-authtoken "$NGROK_AUTHTOKEN" || {
    echo -e "${RED}Failed to configure NGROK authtoken${NC}"
    exit 1
}

# Kill any existing NGROK processes
pkill -f ngrok || true
sleep 2

# Start NGROK in background with proper configuration
echo "Starting NGROK tunnel..."
ngrok http $SERVER_PORT \
    --log=stdout \
    --log-level=debug \
    --host-header="localhost:$SERVER_PORT" > run/ngrok.log 2>&1 &
NGROK_PID=$!

# Save PID for later cleanup
echo $NGROK_PID > run/ngrok.pid

# Check if NGROK process is running
sleep 2
if ! ps -p $NGROK_PID > /dev/null; then
    echo -e "${RED}Error: NGROK failed to start!${NC}"
    echo "Check run/ngrok.log for details:"
    echo "----------------------------------------"
    tail -n 20 run/ngrok.log
    echo "----------------------------------------"
    echo "Common issues:"
    echo "1. Port $SERVER_PORT might be in use"
    echo "2. NGROK authentication token might be invalid"
    echo "3. Network connectivity issues"
    exit 1
fi

# Wait for NGROK to start and API to be available
echo "Waiting for NGROK to initialize..."
MAX_INIT_ATTEMPTS=30
INIT_ATTEMPT=0

while ! curl -s http://localhost:4040/api/tunnels > /dev/null && [ $INIT_ATTEMPT -lt $MAX_INIT_ATTEMPTS ]; do
    # Check if NGROK is still running
    if ! ps -p $NGROK_PID > /dev/null; then
        echo -e "${RED}Error: NGROK process died unexpectedly!${NC}"
        echo "Check run/ngrok.log for details:"
        echo "----------------------------------------"
        tail -n 20 run/ngrok.log
        echo "----------------------------------------"
        exit 1
    fi
    
    INIT_ATTEMPT=$((INIT_ATTEMPT+1))
    echo "Waiting for NGROK API to be available (attempt $INIT_ATTEMPT/$MAX_INIT_ATTEMPTS)..."
    sleep 2
done

if ! curl -s http://localhost:4040/api/tunnels > /dev/null; then
    echo -e "${RED}Error: Failed to initialize NGROK API after $MAX_INIT_ATTEMPTS attempts.${NC}"
    echo "Check run/ngrok.log for details:"
    echo "----------------------------------------"
    tail -n 20 run/ngrok.log
    echo "----------------------------------------"
    echo "Stopping NGROK..."
    kill $NGROK_PID 2>/dev/null || true
    exit 1
fi

# Get NGROK public URL
echo "Getting NGROK public URL..."
NGROK_PUBLIC_URL=""
MAX_ATTEMPTS=30
ATTEMPT=0

while [ -z "$NGROK_PUBLIC_URL" ] && [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    # Check if NGROK is still running
    if ! ps -p $NGROK_PID > /dev/null; then
        echo -e "${RED}Error: NGROK process died while getting public URL!${NC}"
        echo "Check run/ngrok.log for details:"
        echo "----------------------------------------"
        tail -n 20 run/ngrok.log
        echo "----------------------------------------"
        exit 1
    fi
    
    ATTEMPT=$((ATTEMPT+1))
    echo "Trying to get NGROK public URL (attempt $ATTEMPT/$MAX_ATTEMPTS)..."
    
    TUNNELS_DATA=$(curl -s http://localhost:4040/api/tunnels)
    if [ $? -ne 0 ]; then
        echo "Failed to get tunnels data from NGROK API"
        sleep 2
        continue
    fi
    
    NGROK_PUBLIC_URL=$(echo "$TUNNELS_DATA" | jq -r '.tunnels[0].public_url' 2>/dev/null)
    if [ "$NGROK_PUBLIC_URL" == "null" ] || [ -z "$NGROK_PUBLIC_URL" ]; then
        echo "NGROK tunnel not ready yet, waiting..."
        echo "Current tunnels data: $TUNNELS_DATA"
        NGROK_PUBLIC_URL=""
        sleep 2
    fi
done

if [ -z "$NGROK_PUBLIC_URL" ]; then
    echo -e "${RED}Error: Failed to get NGROK public URL after $MAX_ATTEMPTS attempts.${NC}"
    echo "Check run/ngrok.log for details:"
    echo "----------------------------------------"
    tail -n 20 run/ngrok.log
    echo "----------------------------------------"
    echo "Stopping NGROK..."
    kill $NGROK_PID 2>/dev/null || true
    exit 1
fi

echo -e "${GREEN}NGROK public URL: $NGROK_PUBLIC_URL${NC}"

# Update config.yaml with NGROK URL
if [ "$(uname)" = "Darwin" ]; then
    # macOS requires different sed syntax
    sed -i '' "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_PUBLIC_URL\"|" config.yaml
else
    # Linux version
    sed -i "/ngrok:/,/url:/ s|url: \".*\"|url: \"$NGROK_PUBLIC_URL\"|" config.yaml
fi

# Start inferoute-client
echo -e "${BLUE}Starting inferoute-client...${NC}"
inferoute-client -config config.yaml
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

# Now handle config.yaml setup
if [ ! -f "config.yaml" ]; then
    echo -e "${YELLOW}config.yaml not found.${NC}"
    
    # Check if config.yaml.example exists, download if not
    if [ ! -f "config.yaml.example" ]; then
        echo -e "${BLUE}Downloading config.yaml.example...${NC}"
        curl -fsSL -o config.yaml.example https://raw.githubusercontent.com/Inferoute/inferoute-client/main/config.yaml.example
    fi
    
    # Create config.yaml from example
    echo -e "${YELLOW}Creating config.yaml from example...${NC}"
    cp config.yaml.example config.yaml
    echo -e "${YELLOW}Please edit config.yaml to add your NGROK authtoken and other settings.${NC}"
    echo -e "${YELLOW}You can do this by running: nano $INSTALL_DIR/config.yaml${NC}"
    echo -e "${YELLOW}Press Enter to continue after editing the file...${NC}"
    read -p ""
fi

# Check if NGROK authtoken is in config.yaml
NGROK_AUTHTOKEN=$(grep -A 5 "ngrok:" config.yaml | grep "authtoken:" | awk -F'"' '{print $2}')
if [ -z "$NGROK_AUTHTOKEN" ]; then
    echo -e "${RED}Error: NGROK authtoken not found in config.yaml${NC}"
    echo -e "Please add 'authtoken: \"your_ngrok_authtoken_here\"' under the ngrok section in config.yaml"
    echo -e "You can do this by running: nano $INSTALL_DIR/config.yaml"
    exit 1
fi

echo -e "${GREEN}Installation complete!${NC}"
echo -e "${BLUE}Inferoute Client has been installed to:${NC} $INSTALL_DIR"
echo -e "${BLUE}To start inferoute-client with NGROK:${NC}"
echo -e "  cd $INSTALL_DIR"
echo -e "  ./run/start.sh"
echo -e "${BLUE}To stop all services:${NC}"
echo -e "  cd $INSTALL_DIR"
echo -e "  ./run/stop.sh"
echo -e "${YELLOW}Note: NGROK admin interface will be available at http://localhost:4040${NC}"

