###  To Override parameters 

By default we assume:

- your provider type is Ollama  and that it's running on http://locslhost:11434
- Inferoute-client server port will run on port 8080

If you would like to override these default parameters follow the below.


1. To override parameters during auto-install for Linux and OSX

```bash
curl -fsSL https://raw.githubusercontent.com/Inferoute/inferoute-client/main/scripts/install.sh | \
  NGROK_AUTHTOKEN="your-token" \
  PROVIDER_API_KEY="your-key" \
  PROVIDER_TYPE="custom-provider" \
  OLLAMA_URL="http://custom-ollama:11434" \
  SERVER_PORT="9090" \
  bash
```


2. To override parameters during auto-install for Windows 
To pass additional parameters use the below.


3. To override parameters during Docker run

Note that in docker we use `host.docker.internal` which automatically points to the IP address of your Docker host. 

To pass additional parameters use the below.
```
docker run -e NGROK_AUTHTOKEN="your-token" \
           -e PROVIDER_API_KEY="your-key" \
           -e PROVIDER_TYPE="custom-provider" \
           -e OLLAMA_URL="http://host.docker.internal:21434" \
           -e SERVER_PORT="9090" \
           -p 9090:9090 -p 4040:4040 \
           inferoute/inferoute-client:latest
```