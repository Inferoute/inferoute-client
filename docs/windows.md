# Windows

Use this guide when you run the provider client natively on 64-bit Windows. The setup wizard offers **llama.cpp** (CUDA). Native vLLM is not supported. You need an NVIDIA GPU (`nvidia-smi` on PATH).

Setup downloads llama.cpp `b11770` (CUDA 12.4) into `%LOCALAPPDATA%\inferoute\llama.cpp` and serves on `http://127.0.0.1:8081`.

Windows only lists catalog rows with `service_type` `llama_cpp`. Those are GGUF builds of the same models. `provider_type` in the saved config is `llama_cpp`.

## Install

```powershell
irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
```

Or, with the binary already on PATH:

```powershell
inferoute-client setup
```

## Manual config

Copy `config.yaml.example` to `%USERPROFILE%\.config\inferoute\config.yaml` and set:

```yaml
provider:
  api_key: "your_api_key_here"
  provider_type: "llama_cpp"
  engine: "llama_cpp"
  llm_url: "http://127.0.0.1:8081"
  gguf_file: "qwen2.5-7b-instruct-q4_k_m.gguf"
```

llama-server listens on `http://127.0.0.1:8081`. Allow it through **Windows Firewall** if prompted. The connection to Inferoute is outbound and does not need an inbound port.

When the catalog row has a tool-call parser, setup starts llama-server with `--jinja`.
