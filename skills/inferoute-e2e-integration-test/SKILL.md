---
name: inferoute-e2e-integration-test
description: "End-to-end integration test for inferoute-client (JarvisLab GPU + vLLM) against inferoute-node (local Mac consumer). Uses jl CLI to resume/unpause a JarvisLab instance, SSH/exec to start vLLM and inferoute-client, then runs non-streaming and streaming inference from the Mac. Use when the user says 'test inferoute e2e', 'test client with node', 'JarvisLab integration test', or 'verify provider inference'."
argument-hint: "[optional: machine_id, model alias, or 'teardown']"
---

# Inferoute E2E Integration Test (Mac node + JarvisLab provider)

Runs a full-stack smoke test:

```text
[Local Mac]  inferoute-node (consumer API)
      │
      ▼  routes inference + HMAC
[Platform]   inferoute-node / core.inferoute.com
      │
      ▼  Cloudflare tunnel
[JarvisLab]  inferoute-client :8080 → vLLM :8000
```

**This skill does not replace unit tests.** It validates live wiring: tunnel, health, model verification, HMAC, and consumer inference (sync + stream).

## Prerequisites checklist

Run through this before Phase 1. Stop and ask the user for anything missing.

| Requirement | How to verify |
|-------------|---------------|
| `jl` CLI | `command -v jl && jl --version` |
| JarvisLab auth | `jl status` (or `JL_API_KEY` set) |
| SSH key on JarvisLab | `jl ssh-key list` — VM instances require at least one key |
| Paused JarvisLab instance | `jl list --json` — note `machine_id` |
| inferoute-node on Mac | User runs local stack (see `references/inferoute-node-local.md`) |
| Provider API key | From inferoute platform (provider role) |
| Consumer API key | From inferoute platform (inference consumer) |
| Approved model on GPU | Model must exist in approved-builds catalog for `vllm` |
| ngrok on Mac | Exposes local inferoute-node to JarvisLab (`references/ngrok.md`) |
| Mac reachable from JarvisLab | `INFEROUTE_PLATFORM_URL` = ngrok https URL |

### Environment file

Copy and fill `references/env.example` → `~/.config/inferoute/e2e.env` (or repo-local `.env.e2e` — never commit secrets).

```bash
source ~/.config/inferoute/e2e.env
```

Key variables:

| Variable | Example | Purpose |
|----------|---------|---------|
| `JL_MACHINE_ID` | `433049` | Paused JarvisLab instance |
| `INFEROUTE_PLATFORM_URL` | `https://xxxx.ngrok-free.app` | ngrok https URL → Mac docker `:80` |
| `NGROK_CMD` | `ngrok http 80` | Run on Mac before JarvisLab client |
| `INFEROUTE_CONSUMER_URL` | `http://localhost` | inferoute-node docker on port 80 |
| `PROVIDER_API_KEY` | `sk-...` | Provider registration / tunnel |
| `CONSUMER_API_KEY` | `sk-...` | Consumer inference auth |
| `VLLM_MODEL` | `Qwen/Qwen3-0.6B` | `vllm serve` on JarvisLab |
| `INFEROUTE_MODEL_ALIAS` | `Qwen/Qwen3-0.6B` | Use alias from health if different |
| `JL_GPU` | `L4` | Optional override on `jl resume` |

### Network modes

Pick one with the user before starting:

**Mode A — Local inferoute-node (default)**

- Mac runs inferoute-node (docker compose or `go run`).
- **ngrok on Mac** exposes the platform port to the internet; JarvisLab uses that URL as `provider.url`.
- Set `INFEROUTE_PLATFORM_URL` to the ngrok **https** URL (see `references/ngrok.md`).
- `INFEROUTE_CONSUMER_URL` stays `http://localhost` (docker port 80).
- Default `NGROK_CMD="ngrok http 80"`. User can override in `e2e.env` if their ngrok setup differs.

**Mode B — Hosted platform (simpler connectivity)**

- `INFEROUTE_PLATFORM_URL=https://core.inferoute.com`
- Mac only runs consumer client/SDK against the same platform.
- Use when local node networking is not set up yet.

Default to **Mode A** when the user says they run inferoute-node on their Mac.

## Workflow

### Phase 0: Resolve scope

1. Parse `$ARGUMENTS`:
   - `teardown` → jump to Phase 7
   - machine id → set `JL_MACHINE_ID`
   - model alias → set `INFEROUTE_MODEL_ALIAS`
2. `source` the env file; fail fast if required vars are empty.
3. Confirm network mode (A or B) with the user if `INFEROUTE_PLATFORM_URL` is unset or ambiguous.
4. If `NGROK_CMD` differs from default, use the value in `e2e.env`.

### Phase 0b: ngrok + inferoute-node on Mac (Mode A only)

Run on the **user's Mac** before resuming JarvisLab.

1. Start inferoute-node locally (see `references/inferoute-node-local.md`).
2. Start ngrok with the user-provided command:

```bash
# User provides exact command — stored in NGROK_CMD
eval "$NGROK_CMD"
# OR run the literal command the user gave you
```

3. Copy the ngrok **https** forwarding URL into `INFEROUTE_PLATFORM_URL` (no trailing slash).
4. Verify tunnel from Mac:

```bash
curl -sf "$INFEROUTE_PLATFORM_URL/api/health" || curl -sf -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
```

5. After JarvisLab is Running (Phase 1), verify from GPU side:

```bash
jl exec "$JL_MACHINE_ID" -- curl -sf -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL/api/health"
```

**Skip Phase 0b** if using Mode B (`INFEROUTE_PLATFORM_URL=https://core.inferoute.com`).

### Phase 1: Resume JarvisLab instance

```bash
jl get "$JL_MACHINE_ID" --json | jq '{status, gpu, ssh_command}'
jl resume "$JL_MACHINE_ID" --yes ${JL_GPU:+--gpu "$JL_GPU"}
```

Poll until Running (max ~5 min):

```bash
until [ "$(jl get "$JL_MACHINE_ID" --json | jq -r '.status')" = "Running" ]; do
  sleep 10
  echo "waiting for Running..."
done
jl get "$JL_MACHINE_ID" --json | jq '{status, ssh_command, gpu}'
```

Verify GPU:

```bash
jl exec "$JL_MACHINE_ID" -- nvidia-smi
```

### Phase 2: Start vLLM on JarvisLab

Use `jl exec` for idempotent checks; use SSH session (or `tmux` via exec) for long-running servers.

**Check if vLLM already listening:**

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc 'curl -sf http://127.0.0.1:8000/v1/models | head -c 500'
```

**If not running, start in background (persist under `/home`):**

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc '
  mkdir -p /home/inferoute/logs
  if ! pgrep -f "vllm serve" >/dev/null; then
    nohup vllm serve '"$VLLM_MODEL"' \
      --host 0.0.0.0 --port 8000 \
      > /home/inferoute/logs/vllm.log 2>&1 &
  fi
'
```

Wait for model (large models may take several minutes):

```bash
until jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; do
  sleep 15
  echo "waiting for vLLM..."
done
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8000/v1/models | jq .
```

**First-time setup** (only if `vllm` missing on instance):

```bash
jl exec "$JL_MACHINE_ID" -- pip install -q 'vllm>=0.8.0'
```

Prefer a startup script under `/home/inferoute/` if the instance is reused often (see `references/jarvislab-provider-setup.sh`).

### Phase 3: Install and configure inferoute-client on JarvisLab

**Option 1 — Install script (recommended):**

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc '
  export PROVIDER_API_KEY="'"$PROVIDER_API_KEY"'"
  export PROVIDER_TYPE=vllm
  export LLM_URL=http://127.0.0.1:8000
  export SERVER_PORT=8080
  curl -fsSL https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/install.sh | bash
'
```

**Option 2 — Upload local build:**

```bash
jl upload "$JL_MACHINE_ID" ./inferoute-client /home/inferoute/bin/inferoute-client
```

**Write provider config** (adjust `provider.url` to `INFEROUTE_PLATFORM_URL`):

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc 'mkdir -p ~/.config/inferoute && cat > ~/.config/inferoute/config.yaml <<EOF
server:
  port: 8080
  host: "0.0.0.0"
provider:
  api_key: "'"$PROVIDER_API_KEY"'"
  url: "'"$INFEROUTE_PLATFORM_URL"'"
  provider_type: vllm
  llm_url: http://127.0.0.1:8000
logging:
  level: info
EOF'
```

**Start client** (if not already running):

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc '
  mkdir -p /home/inferoute/logs
  if ! pgrep -x inferoute-client >/dev/null; then
    nohup inferoute-client --config ~/.config/inferoute/config.yaml \
      > /home/inferoute/logs/inferoute-client.log 2>&1 &
  fi
'
```

**Sanity checks on JarvisLab:**

```bash
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8080/api/health | jq '{provider_type, models: .data[0:3], tunnel: .cloudflare.url}'
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8080/api/busy
```

### Phase 4: Wait for platform readiness (Mac side)

On the **Mac** (inferoute-node must be running):

1. **Tunnel registered** — health report shows `cloudflare.url` (check JarvisLab health JSON above).
2. **Model verified** — `verification_status` is `verified` for the target alias (may take 1–3 health cycles ≈ 3–9 min after first start).
3. **Model routable** — node lists provider/model for inference.

```bash
# Example: check node health / provider list (adjust path to your inferoute-node API)
curl -s -H "Authorization: Bearer $CONSUMER_API_KEY" \
  "$INFEROUTE_CONSUMER_URL/v1/models" | jq .
```

If verification stays `pending` or `failed`, inspect:

```bash
jl exec "$JL_MACHINE_ID" -- tail -80 /home/inferoute/logs/inferoute-client.log
```

Common causes: weights not in HF cache path, model not in approved-builds, `provider.url` unreachable from JarvisLab.

### Phase 5: Inference tests (Mac consumer → node → client → vLLM)

Run from the **Mac** using `references/test-inference.sh` or the commands below.

**5a. Non-streaming chat completion**

```bash
curl -s "$INFEROUTE_CONSUMER_URL/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$INFEROUTE_MODEL_ALIAS"'",
    "messages": [{"role": "user", "content": "Reply with exactly: inferoute-ok"}],
    "stream": false,
    "max_tokens": 32
  }' | jq .
```

**Pass:** HTTP 200, `choices[0].message.content` contains expected text.

**5b. Non-streaming completion** (if node exposes `/v1/completions`)

```bash
curl -s "$INFEROUTE_CONSUMER_URL/v1/completions" \
  -H "Authorization: Bearer $CONSUMER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$INFEROUTE_MODEL_ALIAS"'",
    "prompt": "Reply with exactly: inferoute-ok",
    "stream": false,
    "max_tokens": 32
  }' | jq .
```

**5c. Streaming chat completion**

```bash
curl -N "$INFEROUTE_CONSUMER_URL/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"$INFEROUTE_MODEL_ALIAS"'",
    "messages": [{"role": "user", "content": "Count from 1 to 5 slowly."}],
    "stream": true,
    "max_tokens": 64
  }'
```

**Pass:** `Content-Type: text/event-stream` (or chunked SSE), multiple `data: {...}` lines, terminal `data: [DONE]`.

**5d. Streaming completion** (if exposed)

Same as 5c against `/v1/completions` with `"stream": true`.

**5e. OpenAI SDK (optional cross-check)**

```bash
python3 - <<'PY'
import os, sys
from openai import OpenAI
client = OpenAI(base_url=os.environ["INFEROUTE_CONSUMER_URL"].rstrip("/") + "/v1",
                api_key=os.environ["CONSUMER_API_KEY"])
stream = client.chat.completions.create(
    model=os.environ["INFEROUTE_MODEL_ALIAS"],
    messages=[{"role": "user", "content": "Say hi in one word"}],
    stream=True, max_tokens=16)
for chunk in stream:
    if chunk.choices and chunk.choices[0].delta.content:
        sys.stdout.write(chunk.choices[0].delta.content)
        sys.stdout.flush()
print()
PY
```

### Phase 6: Report results

Emit a concise table:

| Test | Endpoint | Result | Notes |
|------|----------|--------|-------|
| Health | JarvisLab `/api/health` | pass/fail | tunnel URL, model status |
| Chat sync | `/v1/chat/completions` | pass/fail | |
| Chat stream | `/v1/chat/completions` stream | pass/fail | |
| Completions sync | `/v1/completions` | pass/fail/skip | |
| Completions stream | `/v1/completions` stream | pass/fail/skip | |

**Known limitation (inferoute-client @ main):** the provider client **buffers** LLM responses even when `stream: true` — it does not passthrough SSE to the tunnel. Streaming tests in Phase 5 validate **inferoute-node → consumer** behavior. If provider-side streaming fails but sync works, check whether `origin/streaming-cursor-clean` branch should be deployed on JarvisLab.

Attach on failure:

- Last 50 lines of `/home/inferoute/logs/inferoute-client.log`
- Last 30 lines of `/home/inferoute/logs/vllm.log`
- Consumer response body

Ask the user whether to **pause** the instance or leave running for debugging.

### Phase 7: Teardown

```bash
jl pause "$JL_MACHINE_ID" --yes
```

Optionally stop processes before pause:

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc 'pkill -x inferoute-client || true; pkill -f "vllm serve" || true'
```

**Do not `jl destroy`** unless the user explicitly requests it.

## API surface covered

### Consumer (inferoute-node on Mac)

| Method | Path | Phase |
|--------|------|-------|
| GET | `/v1/models` | 4 |
| POST | `/v1/chat/completions` | 5a, 5c |
| POST | `/v1/completions` | 5b, 5d |

Auth: `Authorization: Bearer $CONSUMER_API_KEY` (confirm header name in your node version).

### Provider (inferoute-client on JarvisLab)

| Method | Path | Phase |
|--------|------|-------|
| GET | `/api/health` | 3, 4 |
| GET | `/api/busy` | 3 |
| POST | `/v1/chat/completions` | indirect via platform |
| POST | `/v1/completions` | indirect via platform |

### Platform (inferoute-node backend)

Exercised indirectly: tunnel request, health push, HMAC validation, model verify, routing.

## Troubleshooting

| Symptom | Likely cause | Action |
|---------|--------------|--------|
| `401` Missing HMAC | Node not signing requests | Check node → provider routing |
| `403` model | Not verified | Wait for health cycles; check approved-builds |
| `502` from client | vLLM down / wrong model id | Check vLLM logs |
| Client can't reach platform | ngrok down or wrong URL | Restart ngrok; re-check `INFEROUTE_PLATFORM_URL`; `jl exec` curl to ngrok URL |
| No tunnel URL | cloudflared / platform error | Client logs, `which cloudflared` on JarvisLab |
| Stream hangs 30s | Client write timeout | Expected on main; try streaming branch |

## References

- `references/env.example` — environment template
- `references/test-inference.sh` — Mac-side test runner
- `references/jarvislab-provider-setup.sh` — optional reusable JarvisLab bootstrap
- `references/ngrok.md` — ngrok on Mac (required for Mode A)
- `references/inferoute-node-local.md` — Mac node startup notes
- `config.yaml.example` (repo root) — provider config fields
- `documentation/technical.md` — client architecture

**Skill location:** `skills/inferoute-e2e-integration-test/SKILL.md` — symlink or copy to `~/.cursor/skills/inferoute-e2e-integration-test/` for Cursor agent discovery.
