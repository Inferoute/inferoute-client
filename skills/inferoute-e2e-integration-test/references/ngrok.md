# ngrok on local Mac (expose inferoute-node to JarvisLab)

JarvisLab cannot reach your Mac directly. **ngrok** exposes inferoute-node (docker on **port 80**) so inferoute-client on JarvisLab can call `provider.url`.

## Flow

```text
JarvisLab inferoute-client  ──HTTPS──►  ngrok public URL  ──►  Mac localhost:80 (docker inferoute-node)
Mac consumer tests          ──HTTP───►  http://localhost     (same docker, no ngrok)
```

## Environment (this setup)

```bash
export JL_MACHINE_ID="433049"
export NGROK_CMD="ngrok http 80"
export INFEROUTE_CONSUMER_URL="http://localhost"

# After ngrok starts — paste the https forwarding URL:
export INFEROUTE_PLATFORM_URL="https://xxxx.ngrok-free.app"
```

## Start ngrok (Mac)

```bash
eval "$NGROK_CMD"
# default: ngrok http 80
```

If you use a custom command (reserved domain, config file, etc.), set `NGROK_CMD` in `e2e.env` and run that instead.

Keep ngrok running for the whole E2E test.

## Verify

**Mac (after docker + ngrok up):**

```bash
curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL/api/health"
curl -s -H "Authorization: Bearer $CONSUMER_API_KEY" "$INFEROUTE_CONSUMER_URL/v1/models" | jq .
```

**JarvisLab (after `jl resume`):**

```bash
jl exec "$JL_MACHINE_ID" -- curl -sf -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL/api/health"
```

Do not start inferoute-client on JarvisLab until the JarvisLab curl succeeds.

## Notes

- `INFEROUTE_PLATFORM_URL` = ngrok **https** URL (for JarvisLab → Mac).
- Consumer inference tests use **localhost** only — ngrok is provider/platform traffic.
- Teardown: `Ctrl+C` ngrok when done.
