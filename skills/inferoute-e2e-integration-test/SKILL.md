---
name: inferoute-e2e-integration-test
description: "End-to-end integration test for inferoute-client (JarvisLab GPU + vLLM) against inferoute-node (local Mac consumer). Uses jl CLI to resume/unpause a JarvisLab instance, SSH/exec to start vLLM and inferoute-client, then runs non-streaming and streaming inference from the Mac. Use when the user says 'test inferoute e2e', 'test client with node', 'JarvisLab integration test', or 'verify provider inference'."
argument-hint: "[optional: machine_id, model alias, 'keep' to skip pause, or 'teardown']"
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
| SSH key on JarvisLab | `jl ssh-key list` — VM instances require at least one key. `jl exec` shells out to `ssh`, so the authorized key must be **offered by ssh** (agent or `~/.ssh/config`). Verify with `ssh -i <key> -o IdentitiesOnly=yes ubuntu@<ip> true`. Wire it in `~/.ssh/config` for the instance IP range (`IdentityFile` + `IdentitiesOnly yes`) so `jl exec` picks it up. See `JL_SSH_KEY` in `env.example`. |
| Paused JarvisLab instance | `jl list --json` — note `machine_id` |
| inferoute-node on Mac | **Always running** in Docker on port 80 — verify with pre-flight curl (see `references/inferoute-node-local.md`); never start docker |
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
| `INFEROUTE_PLATFORM_URL` | `https://saussuritic-ordinarily-sheldon.ngrok-free.dev` | Reserved ngrok domain |
| `NGROK_CMD` | `ngrok http 80 --host-header=localhost --url=saussuritic-ordinarily-sheldon.ngrok-free.dev` | Run on Mac |
| `INFEROUTE_CONSUMER_URL` | `http://localhost` | inferoute-node docker on port 80 |
| `PROVIDER_API_KEY` | `sk-...` | Provider registration / tunnel |
| `CONSUMER_API_KEY` | `sk-...` | Consumer inference auth |
| `VLLM_MODEL` | `Qwen/Qwen3-0.6B` | `vllm serve` on JarvisLab |
| `INFEROUTE_MODEL_ALIAS` | `Qwen/Qwen3-0.6B` | Use alias from health if different |
| `JL_GPU` | `L4` | Optional override on `jl resume` |

### Network modes

Pick one with the user before starting:

**Mode A — Local inferoute-node (default)**

- **Assume inferoute-node Docker is already running** on port 80 — do not start or restart it.
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
   - `keep` → skip auto-pause after Phase 6
   - machine id → set `JL_MACHINE_ID`
   - model alias → set `INFEROUTE_MODEL_ALIAS`
2. `source` the env file; fail fast if required vars are empty.
3. Confirm network mode (A or B) with the user if `INFEROUTE_PLATFORM_URL` is unset or ambiguous.
4. If `NGROK_CMD` differs from default, use the value in `e2e.env`.

### Phase 0b: ngrok on Mac (Mode A only)

Run on the **user's Mac** before resuming JarvisLab. **Do not start inferoute-node** — Docker is always running locally.

1. Verify inferoute-node is reachable (see `references/inferoute-node-local.md` pre-flight).
2. Start ngrok with the user-provided command:

```bash
# User provides exact command — stored in NGROK_CMD
eval "$NGROK_CMD"
# OR run the literal command the user gave you
```

3. Copy the ngrok **https** forwarding URL into `INFEROUTE_PLATFORM_URL` if not already set (reserved domain is preconfigured in `env.example`).
4. Verify tunnel from Mac (`/api/health` is a **client** endpoint, not a platform route — expect any non-000 code, 404 is fine):

```bash
curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
```

5. After JarvisLab is Running (Phase 1), verify from GPU side (non-000 = tunnel up):

```bash
jl exec "$JL_MACHINE_ID" -- curl -s -o /dev/null -w "%{http_code}\n" "$INFEROUTE_PLATFORM_URL"
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

> **Instance ID rotates on resume.** `jl resume` re-provisions the VM — the output prints `Instance ID changed: <old> → <new>` and a new public IP. **Re-export `JL_MACHINE_ID` with the new id** before any further `jl exec`. (If you `source` the env file again, it resets to the old id — re-export after sourcing.)

SSH is not up the instant status flips to Running. Poll it before GPU check:

```bash
until jl exec "$JL_MACHINE_ID" -- true >/dev/null 2>&1; do
  sleep 10
  echo "waiting for SSH..."
done
jl exec "$JL_MACHINE_ID" -- nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader
```

### Phase 2: Start vLLM on JarvisLab

vLLM and inferoute-client are **pre-installed and persistent** on the instance (user `ubuntu`). Never pip-install or run the install script.

| What | Path |
|------|------|
| vLLM binary | `/home/ubuntu/vllm-env/bin/vllm` (`$JL_VLLM_BIN`) |
| Client dir | `/home/ubuntu/inferoute-client` (`$JL_CLIENT_DIR`) — `./inferoute-client` + persistent `config.yaml` |
| Logs | `/home/ubuntu/logs` (`$JL_REMOTE_LOG_DIR`) |

**Two hard-won rules:**

1. **Detect by port, not `pgrep`.** `pgrep -f "vllm serve"` self-matches the `jl exec sh -lc '...'` command string (the pattern is in your own command), giving a false "already running". Check `curl :8000/v1/models` instead.
2. **Launch with `setsid ... </dev/null &`** so the process survives the `jl exec`/SSH session. Plain `nohup ... &` inside `jl exec` can die when the exec channel closes.

**Start vLLM (idempotent, port-based guard):**

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc "
  mkdir -p ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}
  if curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; then
    echo 'vllm already serving'
  else
    setsid ${JL_VLLM_BIN:-/home/ubuntu/vllm-env/bin/vllm} serve '$VLLM_MODEL' \
      --host 0.0.0.0 --port 8000 \
      > ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}/vllm.log 2>&1 </dev/null &
    echo 'vllm launched'
  fi
"
```

Wait for model (large models may take several minutes; **Phase 4 gate 1 repeats this** — do not proceed until it passes):

```bash
until jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; do
  sleep 15
  echo "waiting for vLLM..."
done
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8000/v1/models | jq .
```

`references/jarvislab-provider-setup.sh` does both phases idempotently with these rules baked in.

### Phase 3: Start inferoute-client on JarvisLab

The client binary and its `config.yaml` **persist on the instance** — `config.yaml` already has `provider.url`, `provider_type: vllm`, `llm_url`, and `api_key` set. Do **not** rewrite it unless a value is wrong.

**Only if `provider.url` is stale** (e.g. ngrok domain changed), patch that one line:

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc "sed -i 's#^\(\s*url:\).*#\1 \"$INFEROUTE_PLATFORM_URL\"#' ${JL_CLIENT_DIR:-/home/ubuntu/inferoute-client}/config.yaml"
```

**Start client (idempotent, port-based guard, `setsid`):**

```bash
jl exec "$JL_MACHINE_ID" -- sh -lc "
  CLIENT_DIR=${JL_CLIENT_DIR:-/home/ubuntu/inferoute-client}
  mkdir -p ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}
  if curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1; then
    echo 'client already running'
  else
    setsid \$CLIENT_DIR/inferoute-client --config \$CLIENT_DIR/config.yaml \
      > ${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}/inferoute-client.log 2>&1 </dev/null &
    echo 'client launched'
  fi
"
```

**Sanity checks on JarvisLab** (informational only — do not treat as ready):

```bash
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8080/api/health | jq '{provider_type, models: .data[0:3], tunnel: .cloudflare.url}'
jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8080/api/busy
```

### Phase 4: Wait for readiness (mandatory — blocks Phase 5)

**Do not run Phase 5 until both gates pass.** vLLM model load is usually the long pole (1–10 min depending on model size).

| Gate | What | Typical wait |
|------|------|--------------|
| 1 | vLLM `GET :8000/v1/models` returns 200 | 1–10 min (model size) |
| 2 | inferoute-client `GET :8080/api/health` returns 200 | ~30s–2 min |

Run the blocking wait script from the **Mac** (polls every 15s, fails with log hints on timeout):

```bash
source ~/.config/inferoute/e2e.env
bash skills/inferoute-e2e-integration-test/references/wait-for-ready.sh
```

Manual equivalent:

```bash
# Gate 1 — vLLM (also required at end of Phase 2)
until jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; do
  sleep 15; echo "waiting for vLLM..."
done

# Gate 2 — inferoute-client HTTP
until jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1; do
  sleep 15; echo "waiting for inferoute-client..."
done
```

If inference fails after gates pass (403 model, 502, etc.), inspect:

```bash
jl exec "$JL_MACHINE_ID" -- tail -80 /home/ubuntu/logs/inferoute-client.log
```

Common causes: weights not in HF cache path, model not in approved-builds, `provider.url` unreachable from JarvisLab.

### Phase 5: Inference tests (Mac consumer → node → client → vLLM)

**Prerequisite:** Phase 4 / `wait-for-ready.sh` exited 0 (vLLM + inferoute-client HTTP-ready).

Run from the **Mac** using `references/test-inference.sh` (calls `wait-for-ready.sh` first) or the commands below.

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

- Last 50 lines of `/home/ubuntu/logs/inferoute-client.log`
- Last 30 lines of `/home/ubuntu/logs/vllm.log`
- Consumer response body

**Default: run Phase 7 teardown** (pause JarvisLab) after reporting results.

Skip auto-pause only if the user passed `keep` in arguments or explicitly asked to leave the instance running for debugging.

### Phase 7: Teardown (always pauses JarvisLab)

Run `references/teardown.sh` or equivalent:

```bash
source ~/.config/inferoute/e2e.env
bash skills/inferoute-e2e-integration-test/references/teardown.sh
```

What it does:

- **`jl pause "$JL_MACHINE_ID" --yes`** — stops compute billing. Pausing halts vLLM + inferoute-client too, so there is **no need to stop them first**.

Manual equivalent:

```bash
jl pause "$JL_MACHINE_ID" --yes
```

Also stop ngrok on the Mac (`Ctrl+C` in the ngrok terminal).

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
- `references/teardown.sh` — stop processes + **pause JarvisLab** (run after tests or `test inferoute e2e teardown`)
- `references/wait-for-ready.sh` — **mandatory** blocking polls before inference (vLLM + inferoute-client)
- `references/test-inference.sh` — Mac-side inference smoke tests (runs wait-for-ready first)
- `references/jarvislab-provider-setup.sh` — optional reusable JarvisLab bootstrap
- `references/ngrok.md` — ngrok on Mac (required for Mode A)
- `references/inferoute-node-local.md` — Mac node pre-flight (docker always running)
- `config.yaml.example` (repo root) — provider config fields
- `documentation/technical.md` — client architecture

**Skill location:** `skills/inferoute-e2e-integration-test/SKILL.md` — symlink or copy to `~/.cursor/skills/inferoute-e2e-integration-test/` for Cursor agent discovery.
