###  To Override parameters 

By default we assume:

- your provider type is vLLM and that it's running on http://127.0.0.1:8000
- Inferoute-client server port will run on port 8080

If you would like to override these default parameters follow the below.


1. To override parameters during auto-install for Linux and OSX

```bash
curl -fsSL https://raw.githubusercontent.com/Inferoute/inferoute-client/main/scripts/install.sh | \
  PROVIDER_API_KEY="your-key" \
  PROVIDER_TYPE="custom-provider" \
  LLM_URL="http://127.0.0.1:8000" \
  SERVER_PORT="9090" \
  bash
```


2. To override parameters during auto-install for Windows

```powershell
$env:PROVIDER_API_KEY="your-key"
$env:PROVIDER_TYPE="llama_cpp"
$env:LLM_URL="http://127.0.0.1:1919"
$env:SERVER_PORT="9090"
irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
```

Windows runs llama.cpp. Native vLLM is not supported. Intel Macs are not supported; macOS needs Apple Silicon for vLLM Metal or llama.cpp.


3. To override parameters during Docker run

Note that in docker we use `host.docker.internal` which automatically points to the IP address of your Docker host. 

To pass additional parameters use the below.
```
docker run -e PROVIDER_API_KEY="your-key" \
           -e PROVIDER_TYPE="custom-provider" \
           -e LLM_URL="http://host.docker.internal:21434" \
           -e SERVER_PORT="9090" \
           -p 9090:9090 \
           inferoute/inferoute-client:latest
```
