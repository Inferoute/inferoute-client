# inferoute-node on local Mac (consumer side)

inferoute-node runs in **Docker on port 80** on the Mac.

## Roles

| Component | Where | Purpose |
|-----------|-------|---------|
| inferoute-node | Mac docker `:80` | Consumer API + platform (provider registration) |
| ngrok | Mac | Exposes `:80` to JarvisLab as `INFEROUTE_PLATFORM_URL` |
| inferoute-client | JarvisLab | Provider agent → vLLM |
| vLLM | JarvisLab | `Qwen/Qwen3-0.6B` |

## Startup

```bash
# inferoute-node (your usual docker compose / run command)
docker compose up -d   # listens on localhost:80

# ngrok (separate terminal)
eval "$NGROK_CMD"
# default: ngrok http 80 --host-header=localhost --url=saussuritic-ordinarily-sheldon.ngrok-free.dev
```

## Env for this setup

```bash
export INFEROUTE_CONSUMER_URL="http://localhost"
export NGROK_CMD="ngrok http 80 --host-header=localhost --url=saussuritic-ordinarily-sheldon.ngrok-free.dev"
export INFEROUTE_PLATFORM_URL="https://saussuritic-ordinarily-sheldon.ngrok-free.dev"
export JL_MACHINE_ID="433049"
export VLLM_MODEL="Qwen/Qwen3-0.6B"
export INFEROUTE_MODEL_ALIAS="Qwen/Qwen3-0.6B"   # adjust if health report shows different alias
```

## API testing (curl + API keys only)

No swagger needed. All consumer tests use `Authorization: Bearer $CONSUMER_API_KEY`.

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

## Pre-flight

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost/v1/models \
  -H "Authorization: Bearer $CONSUMER_API_KEY"
curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL/api/health"
```
