###  Manual Installation (For Development)

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