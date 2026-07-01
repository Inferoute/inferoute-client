# ngrok on local Mac (expose inferoute-node to JarvisLab)

JarvisLab cannot reach your Mac directly. **ngrok** exposes inferoute-node (docker on **port 80**) so inferoute-client on JarvisLab can call `provider.url`.

## Flow

```text
JarvisLab inferoute-client  ──HTTPS──►  ngrok public URL  ──►  Mac localhost:80 (docker inferoute-node)
Mac consumer tests          ──HTTP───►  http://localhost     (same docker, no ngrok)
```

## Environment (this setup)

```bash
export NGROK_CMD="ngrok http 80 --host-header=localhost --url=saussuritic-ordinarily-sheldon.ngrok-free.dev"
export INFEROUTE_PLATFORM_URL="https://saussuritic-ordinarily-sheldon.ngrok-free.dev"
export INFEROUTE_CONSUMER_URL="http://localhost"
```

Reserved domain → `INFEROUTE_PLATFORM_URL` is stable; no need to copy from ngrok UI each run.

## Start ngrok (Mac)

```bash
eval "$NGROK_CMD"
```

Keep ngrok running for the whole E2E test. Stop with `Ctrl+C` during teardown.

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

- `--host-header=localhost` required so docker/nginx on port 80 accepts the request.
- Consumer inference tests use **localhost** only — ngrok is provider/platform traffic.
