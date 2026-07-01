# inferoute-node on local Mac (consumer side)

inferoute-node is **not** in the inferoute-client repo. Use your local `inferoute-node` checkout.

## Expected role in E2E test

| Component | Runs on | Purpose |
|-----------|---------|---------|
| inferoute-node | Mac | Consumer OpenAI-compatible API, routing, HMAC signing |
| inferoute-client | JarvisLab | Provider agent, tunnel, vLLM proxy |
| vLLM | JarvisLab | Model inference |

## Typical local startup

Adjust paths/ports to match your node repo:

```bash
cd inferoute-node
# docker compose (preferred if documented in node repo)
docker compose -f docker/env/development.yml up -d

# OR go run (example — confirm in node README)
go run ./cmd/... 
```

Set `INFEROUTE_CONSUMER_URL` to the consumer API port (commonly `8080` or `8081`).

## Critical: JarvisLab must reach the platform URL

When running a **local** node (not core.inferoute.com), set:

```bash
export INFEROUTE_PLATFORM_URL="http://<mac-reachable-host>:<platform-port>"
```

Options:

1. **Tailscale** (recommended) — install on Mac and JarvisLab; use Mac Tailscale IP.
2. **Cloudflare tunnel on Mac** — expose local node HTTPS URL.
3. **Staging** — skip local node; use `https://core.inferoute.com` for both sides.

Verify from JarvisLab before starting inferoute-client:

```bash
jl exec "$JL_MACHINE_ID" -- curl -sf "$INFEROUTE_PLATFORM_URL/api/health" || \
jl exec "$JL_MACHINE_ID" -- curl -sf -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
```

## Consumer API endpoints to confirm in node OpenAPI

Document these from `inferoute-node/docs/swagger.json` (paths may vary by version):

- `GET /v1/models`
- `POST /v1/chat/completions` (sync + `stream: true`)
- `POST /v1/completions` (sync + `stream: true`)

Auth header: typically `Authorization: Bearer <consumer_api_key>`.

## Pre-flight on Mac

```bash
curl -s "$INFEROUTE_CONSUMER_URL/health" || curl -s "$INFEROUTE_CONSUMER_URL/api/health"
```

Update this file once your node local-dev port and health path are confirmed.
