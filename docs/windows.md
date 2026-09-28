# Setup: Windows

Use this guide when you run the provider client natively on 64-bit Windows. The setup wizard offers **Ollama** or **FreeToken**. Native vLLM is not supported.

You need an NVIDIA GPU with **24 GB** of VRAM, and either **32 GB** of RAM plus a **fixed 64 GB page file**, or **64 GB** of RAM.

FreeToken here is the **`ft` CLI** (`ft serve` on port **1919**), installed into `%LOCALAPPDATA%\inferoute\venv-freetoken` with **CUDA PyTorch** (PyPI’s Windows `torch` wheel is CPU-only). It is not **FreeToken Desktop**. If that app is already on the machine, close it before setup — it binds the same API port and its bundled `ft.exe` cannot serve models for Inferoute.

FreeToken does **not** run the full vLLM catalog. Setup and `compatibility` only list models whose catalog `engines` include `freetoken`. Encoder/embedding models (for example `baai/bge-m3`) and older decoder families (Phi-3, Qwen2.5) will be hidden or marked unsupported on Windows. See [FreeToken supported models](https://github.com/FlashML-org/FreeToken/blob/main/docs/models.md). Ollama on Windows still serves the Ollama catalog.

## Memory: where the model lives

FreeToken can hold a MoE model's experts either on the GPU or in system RAM. Setup picks per model:

| Fit on your GPU | Flag setup passes | What it means |
|---|---|---|
| `runs_well` / `fits` | `--moe-backend fused` | Whole model in VRAM |
| `tight` / `too_large`, but `weights × 1.2` fits in system RAM | `--moe-backend auto` | Experts in system RAM, attention + KV cache on the GPU |
| Neither | model hidden | Not offered |

The choice is saved as `provider.moe_strategy` in `config.yaml` and reused on auto-start. `inferoute-client compatibility` shows it in the `MOE` column.

**Offload needs Windows commit space.** Windows limits a process to RAM + page file. With `auto`, FreeToken commits roughly the full weight size in host memory during load. For `openai/gpt-oss-20b` (12.8 GiB weights) on a 24 GB card:

- 32 GB RAM: set a **fixed 64 GB page file** (System Properties → Advanced → Performance → Virtual memory). Without it the load fails with `OSError 1455: The paging file is too small`.
- 64 GB RAM: default page file is fine.

`fused` on a 24 GB card fails for this model: FreeToken's loader clones tensors while copying to the GPU and CUDA-OOMs above ~22 GiB. That is why setup only trusts `fused` when the VRAM score is `fits` or better.

## Quick install (recommended)

1. Get your provider API key from the [Inferoute platform](https://core.inferoute.com).
2. In **PowerShell**:

   ```powershell
   irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
   ```

The script installs **cloudflared** and **inferoute-client** to `%LOCALAPPDATA%\inferoute\bin`, runs `inferoute-client setup`, and adds that folder to your user **PATH**. It does **not** require Administrator. Re-run `inferoute-client setup` anytime to change engine, model, or API key.

3. Start the client from **Start Menu → Inferoute → Inferoute Client**, or from a **new** terminal:

   ```powershell
   inferoute-client
   ```

On Windows the client runs in the **notification area** by default. The PowerShell prompt waits until the local dashboard is up, then returns; closing that window does **not** stop the client. A notification appears when the client starts.

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

Install the [NVIDIA driver](https://www.nvidia.com/drivers) so `nvidia-smi` is on **PATH**. You need **24 GB** of VRAM and either **32 GB** of RAM with a **fixed 64 GB page file**, or **64 GB** of RAM. Then the client reports GPU name, VRAM, and busy status (utilization above 20%). Without `nvidia-smi` the client still runs; GPU fields are empty and busy is not detected.

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
