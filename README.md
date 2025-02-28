# Inferoute Provider Client

The Inferoute Provider Client is a lightweight Go service that runs on Ollama provider machines. It handles health monitoring, reporting, and inference request handling.

## Features

- **Health Monitoring & Reporting**: Collects local metrics (GPU type, number of GPUs, utilization stats, models available) and reports them to the central system.
- **Inference Request Handling**: Forwards inference requests to the local Ollama instance after checking GPU availability.
- **HMAC Validation**: Validates HMACs on incoming requests to ensure they are legitimate.
- **OpenAI API Compatibility**: Implements the OpenAI API for chat completions and completions.

## Requirements

- NVIDIA GPU with nvidia-smi installed (for GPU monitoring)
- Ollama running locally
- jq (installed automatically by the script if missing)

## Installation

### Linux/OSX

```bash
curl -fsSL https://raw.githubusercontent.com/sentnl/inferoute-client/main/scripts/install.sh | bash
```

After installation, start the client with:
```bash
./run/start.sh
```

### Windows

### Option 2: Manual Download and Install

1. Download the install script:
   ```
   curl -O https://raw.githubusercontent.com/sentnl/inferoute-client/main/scripts/install.sh
   chmod +x install.sh
   ```

2. Create your configuration file:
   ```
   curl -O https://raw.githubusercontent.com/sentnl/inferoute-client/main/config.yaml.example
   cp config.yaml.example config.yaml
   ```

3. Edit the configuration file to set your provider API key and NGROK authtoken:
   ```
   nano config.yaml
   ```

4. Run the install script:
   ```
   ./install.sh
   ```

5. Start the client:
   ```
   ./run/start.sh
   ```

### Option 3: Manual Installation (For Development)

1. Install Go 1.21 or higher if you want to build from source.

2. Clone the repository:
   ```
   git clone https://github.com/sentnl/inferoute-client.git
   cd inferoute-client
   ```

3. Copy the example configuration file:
   ```
   cp config.yaml.example config.yaml
   ```

4. Edit the configuration file to set your provider API key and other settings:
   ```
   nano config.yaml
   ```

5. Build the client:
   ```
   go build -o inferoute-client ./cmd
   ```

### Option 4: Download Pre-built Binary

1. Download the latest binary for your platform from the [Releases page](https://github.com/sentnl/inferoute-client/releases).

2. Extract the binary:
   ```
   unzip inferoute-client-*.zip
   chmod +x inferoute-client-*
   mv inferoute-client-* inferoute-client
   ```

3. Create and configure your `config.yaml` file.

## Usage

1. Start the client:
   ```
   ./inferoute-client
   ```

   Or with a custom configuration file:
   ```
   ./inferoute-client -config /path/to/config.yaml
   ```

2. The client will start a server on the configured port (default: 8080) and begin sending health reports to the central system.

## API Endpoints

- **GET /health**: Returns the current health status of the provider, including GPU information and available models.
- **GET /busy**: Returns whether the GPU is currently busy (TRUE or FALSE).
- **POST /v1/chat/completions**: OpenAI-compatible chat completions API endpoint.
- **POST /v1/completions**: OpenAI-compatible completions API endpoint.

## Configuration

The configuration file (`config.yaml`) contains the following settings:

- **server**: Server configuration (port, host)
- **ollama**: Ollama configuration (URL)
- **provider**: Provider configuration (API key, central system URL)
- **ngrok**: NGROK configuration (URL, authtoken)

## Docker Setup

### OSX

Install Docker Desktop

### Windows 

Install Docker Desktop

### Linux

Follow the [official Docker installation instructions](https://docs.docker.com/engine/install/).

## License

This project is licensed under the MIT License - see the LICENSE file for details. 



## Setup 

### Docker 

#### OSX

Install Docker Desktop

#### Windows 

Install Docker Desktop 



### Run without Docker
