# Setup: Windows

Use this guide when you run the provider client natively on 64-bit Windows with [Ollama](https://ollama.com). vLLM is not supported on native Windows.

## Quick install (recommended)

1. Install [Ollama for Windows](https://ollama.com) and pull at least one model.
2. Get your provider API key from the [Inferoute platform](https://core.inferoute.com).
3. In **PowerShell**:

   ```powershell
   $env:PROVIDER_API_KEY="your-key"; irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
   ```

The script installs **cloudflared** and **inferoute-client** to `%LOCALAPPDATA%\inferoute\bin`, writes config to `%USERPROFILE%\.config\inferoute\config.yaml`, and adds that folder to your user **PATH**. It does **not** require Administrator.

4. Start the client from **Start Menu → Inferoute → Inferoute Client**, or from a **new** terminal:

   ```powershell
   inferoute-client
   ```

On Windows the client runs in the **notification area** by default. The PowerShell prompt returns immediately; closing that window does **not** stop the client. A notification appears when the client starts.

Right-click the Inferoute icon → **Open dashboard** to view live status in your browser (same information as the Linux/macOS terminal UI). Use **Quit** on that menu to stop the client.

To keep the old terminal dashboard instead of the tray:

```powershell
inferoute-client --console
```

Default config: `%USERPROFILE%\.config\inferoute\config.yaml`. Logs: `%USERPROFILE%\.local\state\inferoute\log`.

If SmartScreen says **Windows protected your PC**, choose **More info** → **Run anyway** (the GitHub binary is not Authenticode-signed).

## Ollama on Windows

Ollama is the supported backend on Windows.

If the client runs in Docker and Ollama on the host, see [Setup: Ollama](setup-ollama.md#windows). For a native install, `http://localhost:11434` is the default.

Allow Ollama through **Windows Firewall** if prompted. The Inferoute Cloudflare tunnel is outbound HTTPS and does not need an inbound port.

## GPU monitoring

Install the [NVIDIA driver](https://www.nvidia.com/drivers) so `nvidia-smi` is on **PATH**. Then the client reports GPU name, VRAM, and busy status (utilization above 20%). Without `nvidia-smi` the client still runs; GPU fields are empty and busy is not detected.

`inferoute-client compatibility` uses the same `nvidia-smi` data, or system RAM if no NVIDIA GPU is present.

## Manual install

1. Download `inferoute-client-windows-amd64.zip` from [GitHub Releases](https://github.com/inferoute/inferoute-client/releases).
2. Install **cloudflared**: download `cloudflared-windows-amd64.exe` from [Cloudflare releases](https://github.com/cloudflare/cloudflared/releases) (or `winget install Cloudflare.cloudflared`).
3. Place both executables on **PATH**.
4. Copy `config.yaml.example` to `%USERPROFILE%\.config\inferoute\config.yaml` and set `api_key`, `provider_type: ollama`, and `llm_url`.
5. Run `inferoute-client`.

The client requests a Cloudflare tunnel from the platform and runs **cloudflared** for you.

## Build from source

Requires [Go 1.22+](https://go.dev/dl/) on PATH. From a clone of this repo:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1
```

Or double-click `scripts\build.bat`. Writes `inferoute-client.exe` in the repo root.

## Related

- [Installation](installation.md)
- [Configuration](configuration.md)
- [Setup: Ollama](setup-ollama.md)
- [FAQ](faq.md)
