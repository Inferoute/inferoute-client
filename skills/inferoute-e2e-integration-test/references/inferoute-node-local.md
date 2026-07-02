# inferoute-node on local Mac (consumer side)

inferoute-node runs in **Docker on port 80** on the Mac. **Always assume it is already running** — never start, stop, or restart Docker as part of this skill.

## Roles

| Component | Where | Purpose |
|-----------|-------|---------|
| inferoute-node | Mac docker `:80` | Consumer API + platform (provider registration) |
| ngrok | Mac | Exposes `:80` to JarvisLab as `INFEROUTE_PLATFORM_URL` |
| inferoute-client | JarvisLab | Provider agent → vLLM |
| vLLM | JarvisLab | `Qwen/Qwen3-0.6B` |

## Environment

All variables come from `~/.config/inferoute/e2e.env` — source it once before any commands in this doc:

```bash
source ~/.config/inferoute/e2e.env
```

Template: `references/env.example`. Key vars for this setup: `INFEROUTE_CONSUMER_URL`, `NGROK_CMD`, `INFEROUTE_PLATFORM_URL`, `CONSUMER_API_KEY`.

## Pre-flight (verify only — do not start docker)

```bash
source ~/.config/inferoute/e2e.env

curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_CONSUMER_URL/v1/models" \
  -H "Authorization: Bearer $CONSUMER_API_KEY"
```

Expect `200`. If not, stop and tell the user their local stack is down — do not attempt to fix it from this skill.

## ngrok (separate terminal — only thing to start on Mac)

```bash
source ~/.config/inferoute/e2e.env
eval "$NGROK_CMD"
```

Then verify platform URL:

```bash
curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL/api/health"
```

## API testing (curl + API keys only)

Requires `source ~/.config/inferoute/e2e.env`. No swagger needed — all consumer tests use `Authorization: Bearer $CONSUMER_API_KEY`.

```bash
# List models
curl -s -H "Authorization: Bearer $CONSUMER_API_KEY" \
  "$INFEROUTE_CONSUMER_URL/v1/models" | jq .

# Chat (sync)
curl -s -H "Authorization: Bearer $CONSUMER_API_KEY" \
  -H "Content-Type: application/json" \
  "$INFEROUTE_CONSUMER_URL/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"hi"}],"stream":false,"max_tokens":32}'

# Chat (stream) — use -N
curl -N -H "Authorization: Bearer $CONSUMER_API_KEY" \
  -H "Content-Type: application/json" \
  "$INFEROUTE_CONSUMER_URL/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"hi"}],"stream":true,"max_tokens":32}'
```

Full script: `references/test-inference.sh`
