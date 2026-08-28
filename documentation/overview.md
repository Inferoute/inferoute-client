# Inferoute Provider Client — overview

This document is for **non-technical managers**. It explains what the provider client is, what it does on a GPU machine, and how it fits Inferoute. Implementation detail lives in [technical.md](technical.md). Platform-wide product context is in **inferoute-node** `documentation/overview.md`.

## What it is

The Inferoute Provider Client is a small program that runs on a provider’s GPU machine, next to **Ollama** or **vLLM**. It is how a cluster joins the Inferoute network.

It does not replace the inference engine. Ollama/vLLM still load weights and generate tokens. The client is the **front door**: health, pricing, model checks, and a locked-down proxy that only Inferoute is allowed to call.

Providers install it with a one-liner (Linux/macOS) or a PowerShell script (Windows) after creating a cluster and copying the API key from the dashboard.

## What it does

1. **Opens a private path to the platform.** At startup it asks Inferoute for a Cloudflare tunnel and runs `cloudflared`. Inferoute can reach the machine without any inbound firewall ports. The public hostname looks like `user-clustername` on the Inferoute tunnel domain.
2. **Tells the platform the machine is alive.** Every three minutes (and immediately when the GPU goes busy or idle) it reports loaded models, GPU info, and the tunnel URL. If reports stop for five minutes, the platform stops sending work.
3. **Registers models and default prices.** On first start it publishes whatever Ollama/vLLM has loaded, using market-average prices. The provider then sets real prices in the dashboard.
4. **Proves the weights match the name.** For marketplace-listed models, the client measures local files (Ollama digest, or a hash of vLLM weight files) and the platform decides `verified` / `failed`. Unverified allowlisted models are not served.
5. **Runs inference only for real Inferoute traffic.** Incoming chat/completion requests must carry a one-time request token. The client checks it with the platform, then forwards the body to local Ollama or vLLM. Random internet callers cannot use the GPU.
6. **Keeps one job on the GPU at a time (by default).** A second *different* conversation is rejected so the platform can try another cluster. A *follow-up turn of the same conversation* waits (up to ~90 seconds) so the chat can reuse memory already on that GPU.

There is also a **compatibility** command that does not start the service. It looks at local RAM/VRAM and the public model catalog and says which approved models will fit (`runs_well` / `fits` / `tight` / `too_large`). Useful before buying or downloading a model. No API key required.

## What the operator sees

- **Linux / macOS:** a terminal dashboard that refreshes every few seconds (session, tunnel URL, GPU, model approval, recent requests).
- **Windows:** the client lives in the notification area by default. Closing the terminal does not stop it. “Open dashboard” shows the same status in a browser at `http://127.0.0.1:8080/`. `--console` keeps the old terminal UI.

Logs rotate under `~/.local/state/inferoute/log` (Windows: under the user’s state directory).

## Supported setups

| Platform | Typical engine | GPU picture |
|----------|----------------|-------------|
| Linux + NVIDIA | Ollama or vLLM | Full `nvidia-smi` (utilization, VRAM) |
| Windows amd64 | Ollama | `nvidia-smi` when the NVIDIA driver is present |
| macOS | Ollama | Basic GPU identity; busy = “a request is already running” |

Hardware bar for a useful cluster: NVIDIA GPU with **8 GB+** VRAM. Apple Silicon can run small models on unified memory; the compatibility command scores that conservatively.

Docker is supported, but the local LLM must be reachable from the container (`host.docker.internal` is the usual pattern). `cloudflared` must be on the PATH; install scripts put it there.

## How this relates to money and routing

The client does **not** bill anyone. It reports health and proxies tokens. The platform:

- chooses this cluster (or not) using health, price, verification, and whether this GPU already has the conversation
- holds and later charges the consumer wallet
- credits the cluster owner

If this machine is busy with someone else’s chat, rejecting quickly is the correct behavior: Inferoute should fail over. If it is busy finishing the *same* chat, waiting is the correct behavior: that is how follow-up turns stay fast.

## What it does not do

- It does not train models or download weights for you (except whatever Ollama/vLLM already do).
- It does not expose a public OpenAI API to the internet. The tunnel is for Inferoute only; requests without a valid platform token are rejected.
- It does not stream tokens back to the consumer today — the full reply is buffered, then returned. The platform can stream when the client does; this is a known gap.
- It cannot stop a malicious fork of the client. Verification catches wrong files on an honest install.

## In summary

The provider client is the software that turns a GPU box into an Inferoute cluster: tunnel in, health out, models verified, inference proxied to Ollama or vLLM, one conversation at a time unless you raise the cap. Install it, keep Ollama/vLLM running, set prices in the dashboard.
