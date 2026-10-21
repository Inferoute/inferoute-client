# Inferoute Provider Client — overview

This document is for **non-technical managers**. It explains what the provider client is, what it does on a GPU machine, and how it fits Inferoute. Implementation detail lives in [technical.md](technical.md). Platform-wide product context is in **inferoute-node** `documentation/overview.md`.

## What it is

The Inferoute Provider Client is a small program that runs on a provider’s GPU machine, next to **vLLM**, **vLLM Metal**, or **FreeToken**. It is how a cluster joins the Inferoute network.

It does not replace the inference engine. The engine still loads weights and generates tokens. The client is the **front door**: health, pricing, model checks, and a locked-down proxy that only Inferoute is allowed to call.

Providers install it with a one-liner (Linux/macOS) or a PowerShell script (Windows) after creating a cluster and copying the API key from the dashboard.

## What it does

1. **Opens a private path to the platform.** At startup it opens an outbound connection to Inferoute and reconnects if that connection drops. Inferoute sends inference back over it. No extra binary, no inbound ports, no public URL.
2. **Tells the platform the machine is alive.** Every three minutes (and immediately when the GPU goes busy or idle) it reports loaded models and GPU info. If reports stop for five minutes, the platform stops sending work.
3. **Registers models and default prices.** On first start it publishes whatever the local engine has loaded, using market-average prices. The provider then sets real prices in the dashboard.
4. **Proves the weights match the name.** For marketplace-listed models, the client hashes local weight files and the platform decides `verified` / `failed`. Unverified allowlisted models are not served.
5. **Runs inference only for real Inferoute traffic.** Incoming chat/completion requests must carry a one-time request token. The client checks it with the platform, then forwards the body to the local engine. Random internet callers cannot use the GPU.
6. **Keeps one job on the GPU at a time (by default).** A second *different* conversation is rejected so the platform can try another cluster. A *follow-up turn of the same conversation* waits (up to ~90 seconds) so the chat can reuse memory already on that GPU.

There is also a **compatibility** command that does not start the service. It looks at local RAM/VRAM and the public model catalog and says which approved models will fit (`runs_well` / `fits` / `tight` / `too_large`). On Windows it also hides vLLM models that are not tagged for FreeToken. Useful before buying or downloading a model. No API key required.

## What the operator sees

- **Linux / macOS:** a terminal dashboard that refreshes every few seconds (session, connection, GPU, model approval, recent requests).
- **Windows:** the client lives in the notification area by default. From a terminal, the prompt waits until `http://127.0.0.1:8080/` is up, then returns. Closing the terminal does not stop it. “Open dashboard” shows the same status in a browser. `--console` keeps the old terminal UI.

Logs rotate under `~/.local/state/inferoute/log` (Windows: under the user’s state directory).

## Supported setups

| Platform | Typical engine | GPU picture |
|----------|----------------|-------------|
| Linux + NVIDIA | vLLM | Full `nvidia-smi` (utilization, VRAM) |
| Windows amd64 | FreeToken | `nvidia-smi` when the NVIDIA driver is present |
| macOS (Apple Silicon) | vLLM Metal | Basic GPU identity; busy = “a request is already running” |

Hardware bar for a useful cluster: **32 GB** of system memory and an NVIDIA GPU with **24 GB+** VRAM on Linux; on Windows **32 GB** of RAM plus a **fixed 64 GB page file** (or **64 GB** of RAM) and **24 GB+** VRAM; or Apple Silicon with **48 GB+** unified memory. The compatibility command scores that conservatively.

Docker is supported, but the local LLM must be reachable from the container (`host.docker.internal` is the usual pattern). The connection to Inferoute is outbound, so the client does not need a published port.

## How this relates to money and routing

The client does **not** bill anyone. It reports health and proxies tokens. The platform:

- chooses this cluster (or not) using health, price, verification, and whether this GPU already has the conversation
- holds and later charges the consumer wallet
- credits the cluster owner

If this machine is busy with someone else’s chat, rejecting quickly is the correct behavior: Inferoute should fail over. If it is busy finishing the *same* chat, waiting is the correct behavior: that is how follow-up turns stay fast.

## What it does not do

- It does not train models or download weights for you (except whatever the local engine already does).
- It does not expose a public OpenAI API to the internet. The tunnel is for Inferoute only; requests without a valid platform token are rejected.
- `stream: true` is proxied. The client copies the engine response and flushes as bytes arrive, so the consumer gets SSE.
- It cannot stop a malicious fork of the client. Verification catches wrong files on an honest install.

## In summary

The provider client is the software that turns a GPU box into an Inferoute cluster: tunnel in, health out, models verified, inference proxied to vLLM, vLLM Metal, or FreeToken, one conversation at a time unless you raise the cap. Install it, keep the engine running, set prices in the dashboard.
