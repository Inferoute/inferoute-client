###  Manual Installation

1. Install Go 1.21 or higher if you want to build from source.

2. Clone the repository:
   ```bash
   git clone https://github.com/sentnl/inferoute-client.git
   cd inferoute-client
   ```

3. Copy the example configuration file:
   ```bash
   cp config.yaml.example config.yaml
   ```

4. Edit the configuration file to set your provider API key and other settings:
   ```bash
   nano config.yaml
   ```

5. Build the client:
   ```bash
   go build -o inferoute-client ./cmd
   ```

6. Make sure NGROK is installed and running:

https://ngrok.com/docs/getting-started/

7. Update config.yaml with your NGROK auth token and NGROK URL
8. Run the Inferoute-client passing the location of your configuration file 
```bash
inferoute-client --config config.yaml
```



