# ngrok on local Mac (expose inferoute-node to JarvisLab)

JarvisLab cannot reach your Mac directly. **ngrok** exposes inferoute-node (docker on **port 80**) so inferoute-client on JarvisLab can call `provider.url`.

## Flow

```text
JarvisLab inferoute-client  ──HTTPS──►  ngrok public URL  ──►  Mac localhost:80 (docker inferoute-node)
Mac consumer tests          ──HTTP───►  http://localhost     (same docker, no ngrok)
```

## Environment

All variables come from `~/.config/inferoute/e2e.env`:

```bash
source ~/.config/inferoute/e2e.env
```

Relevant vars: `NGROK_CMD`, `INFEROUTE_PLATFORM_URL`, `INFEROUTE_CONSUMER_URL`. Reserved domain → `INFEROUTE_PLATFORM_URL` is stable; no need to copy from ngrok UI each run.

## Start ngrok (Mac)

```bash
source ~/.config/inferoute/e2e.env
eval "$NGROK_CMD"
```

Keep ngrok running for the whole E2E test. Stop with `Ctrl+C` during teardown.

## Verify

`/api/health` is an **inferoute-client** endpoint (JarvisLab only) — it does **not** exist on the node/platform. Reachability of the platform through ngrok = any HTTP response (even 404), not a 200.

**Mac (docker already running; after ngrok up):**

```bash
# Tunnel reachable = non-000 status (node has no /api/health; 404 is fine)
curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
# Local consumer API (not via ngrok)
curl -s -H "Authorization: Bearer $CONSUMER_API_KEY" "$INFEROUTE_CONSUMER_URL/v1/models" | jq .
```

**JarvisLab (after `jl resume`):**

```bash
# Confirm JarvisLab can reach the platform (any non-000 code = tunnel up)
jl exec "$JL_MACHINE_ID" -- curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
```

Do not start inferoute-client on JarvisLab until the JarvisLab curl returns a non-000 code.

## Notes

- `--host-header=localhost` required so docker/nginx on port 80 accepts the request.
- `/api/health` and `/api/busy` are **client** endpoints (port 8080 on JarvisLab), not platform routes.
- Consumer inference tests use **localhost** only — ngrok is provider/platform traffic.
