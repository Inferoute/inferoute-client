#!/usr/bin/env bash
# End-to-end inferoute test, fully automated:
#   ngrok -> resume JarvisLab -> build client -> (vLLM phase) -> (Ollama phase)
#   -> run inference tests (Mac consumer) -> pause JarvisLab
#
# Two backends are exercised in sequence against the SAME client codebase:
#   1. vLLM     serving $VLLM_MODEL      (client config: $CLIENT_CONFIG)
#   2. Ollama   serving $OLLAMA_MODEL    (client config: $OLLAMA_CLIENT_CONFIG)
# vLLM is stopped before Ollama starts so the GPU is free for the second phase.
#
# The client is rebuilt from source on JarvisLab (scripts/build.sh) before the
# phases run, so every e2e uses the latest client binary.
#
# The instance is ALWAYS paused on exit (success, failure, or Ctrl-C) unless KEEP=1.
#
# Usage:
#   ./run-e2e.sh                 # full run (vLLM + Ollama), pause at the end
#   KEEP=1 ./run-e2e.sh          # leave instance running for debugging
#   RUN_OLLAMA=0 ./run-e2e.sh    # vLLM only
#   RUN_VLLM=0 ./run-e2e.sh      # Ollama only
#   ./run-e2e.sh teardown        # just pause the instance and exit
#
# Config comes from references/.env next to this script (override path with E2E_ENV).
# references/.env is git-ignored — never commit filled secrets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/references/.env}"
# shellcheck source=/dev/null
[ -f "$E2E_ENV" ] && source "$E2E_ENV"

# Instance id is discovered from `jl list --json` by name, never hard-coded.
JL_NAME="${JL_NAME:-Inferoute-client1}"
resolve_machine_id() {
  jl list --json 2>/dev/null \
    | jq -r --arg n "$JL_NAME" 'map(select(.name==$n)) | .[0].machine_id // empty'
}
JL_MACHINE_ID="$(resolve_machine_id)"
[ -n "$JL_MACHINE_ID" ] || { printf '\033[1;31m[e2e] no JarvisLab instance named %s (check: jl list)\033[0m\n' "$JL_NAME" >&2; exit 1; }

: "${INFEROUTE_PLATFORM_URL:?set INFEROUTE_PLATFORM_URL in e2e.env}"
: "${INFEROUTE_CONSUMER_URL:?set INFEROUTE_CONSUMER_URL in e2e.env}"
: "${CONSUMER_API_KEY:?set CONSUMER_API_KEY in e2e.env}"
: "${VLLM_MODEL:?set VLLM_MODEL in e2e.env}"
: "${INFEROUTE_MODEL_ALIAS:?set INFEROUTE_MODEL_ALIAS in e2e.env}"

VLLM_BIN="${JL_VLLM_BIN:-/home/ubuntu/vllm-env/bin/vllm}"
CLIENT_DIR="${JL_CLIENT_DIR:-/home/ubuntu/inferoute-client}"
LOG_DIR="${JL_REMOTE_LOG_DIR:-/home/ubuntu/logs}"
NGROK_LOG="${NGROK_LOG:-/tmp/inferoute-ngrok.log}"
NGROK_CMD="${NGROK_CMD:-ngrok http 80 --host-header=localhost --url=${INFEROUTE_PLATFORM_URL#https://}}"
VLLM_WAIT_SEC="${VLLM_WAIT_SEC:-600}"
CLIENT_WAIT_SEC="${CLIENT_WAIT_SEC:-180}"
RUNNING_WAIT_SEC="${RUNNING_WAIT_SEC:-360}"
SSH_WAIT_SEC="${SSH_WAIT_SEC:-240}"
PROVIDER_WAIT_SEC="${PROVIDER_WAIT_SEC:-180}"
DB_CONTAINER="${DB_CONTAINER:-cockroachdb}"
DB_NAME="${DB_NAME:-inferoute}"

# Backend / client-config knobs (see references/env.example).
CLIENT_CONFIG="${CLIENT_CONFIG:-config.yaml}"
OLLAMA_CLIENT_CONFIG="${OLLAMA_CLIENT_CONFIG:-config-ollama.yaml}"
OLLAMA_MODEL="${OLLAMA_MODEL:-qwen3:0.6b}"
OLLAMA_MODEL_ALIAS="${OLLAMA_MODEL_ALIAS:-gguf/qwen3:0.6b}"
OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
OLLAMA_PORT="${OLLAMA_PORT:-11434}"
CLIENT_GIT_PULL="${CLIENT_GIT_PULL:-1}"
CLIENT_GIT_BRANCH="${CLIENT_GIT_BRANCH:-main}"
RUN_VLLM="${RUN_VLLM:-1}"
RUN_OLLAMA="${RUN_OLLAMA:-1}"

KEEP="${KEEP:-0}"
STARTED_NGROK=0
OVERALL=0
TUNNEL=""

log()  { printf '\n\033[1;36m[e2e] %s\033[0m\n' "$*"; }
step() { printf '\033[1;35m[e2e] ── %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[e2e] %s\033[0m\n' "$*" >&2; }
die()  { printf '\033[1;31m[e2e] %s\033[0m\n' "$*" >&2; exit 1; }

finish() {
  local code=$?
  trap - EXIT INT TERM
  if [ "$KEEP" = "1" ]; then
    warn "KEEP=1 — leaving JarvisLab $JL_MACHINE_ID running (pause manually: jl pause $JL_MACHINE_ID --yes)"
  else
    log "Pausing JarvisLab $JL_MACHINE_ID"
    jl pause "$JL_MACHINE_ID" --yes || warn "pause FAILED — run: jl pause $JL_MACHINE_ID --yes"
  fi
  [ "$STARTED_NGROK" = "1" ] && { log "Stopping ngrok (we started it)"; pkill -f "ngrok http" 2>/dev/null || true; }
  [ "$code" = "0" ] && log "DONE ✓" || warn "EXIT code $code"
  exit "$code"
}

# 'teardown' shortcut: just pause and exit.
if [ "${1:-}" = "teardown" ]; then
  log "Teardown: pausing $JL_MACHINE_ID"
  jl pause "$JL_MACHINE_ID" --yes
  exit 0
fi

# ── shared helpers ───────────────────────────────────────────────────────────
wait_gate() {
  local desc="$1" max="$2"; shift 2
  local start=$SECONDS
  until "$@"; do
    (( SECONDS - start >= max )) && { warn "TIMEOUT: $desc (>${max}s)"; return 1; }
    echo "  ...waiting $desc"; sleep 15
  done
  log "$desc ready"
}
vllm_ready()   { jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8000/v1/models  >/dev/null 2>&1; }
client_ready() { jl exec "$JL_MACHINE_ID" -- curl -sf http://127.0.0.1:8080/api/health >/dev/null 2>&1; }
ollama_ready() { jl exec "$JL_MACHINE_ID" -- sh -lc "curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags 2>/dev/null | grep -q '$OLLAMA_MODEL'"; }

# Kill whatever is listening on a TCP port (iproute2 only, no psmisc dependency).
kill_port() {
  local port="$1"
  jl exec "$JL_MACHINE_ID" -- sh -lc "
    pids=\$(ss -Htlnp 'sport = :$port' 2>/dev/null | grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u)
    [ -n \"\$pids\" ] && kill \$pids 2>/dev/null || true
    sleep 2
    pids=\$(ss -Htlnp 'sport = :$port' 2>/dev/null | grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u)
    [ -n \"\$pids\" ] && kill -9 \$pids 2>/dev/null || true
    true
  "
}

# Provider row is keyed by its tunnel URL (providers.api_url). Uses global TUNNEL.
provider_status() {
  docker exec -i "$DB_CONTAINER" cockroach sql --insecure -d "$DB_NAME" --format=csv \
    -e "SELECT health_status FROM providers WHERE api_url='$TUNNEL' AND deleted_at IS NULL LIMIT 1;" \
    2>/dev/null | tail -n +2 | head -1
}
provider_green() { [ "$(provider_status)" = "green" ]; }

wait_provider_green() {
  if [ "${SKIP_DB_GATE:-0}" = "1" ]; then
    warn "SKIP_DB_GATE=1 — not waiting for provider green"
  elif [ -z "$TUNNEL" ]; then
    warn "no tunnel URL in client health — skipping DB gate"
  else
    log "waiting for provider (api_url=$TUNNEL) to go green"
    wait_gate "provider green" "$PROVIDER_WAIT_SEC" provider_green \
      || die "provider never went green (current: $(provider_status | sed 's/^$/none/'))"
  fi
}

# (re)start the client with a given config, then health + DB gate + inference tests.
# The backend it points at must already be serving. args: label, config_file, alias.
run_client_phase() {
  local label="$1" config="$2" alias="$3"

  step "[$label] (re)start inferoute-client with $config"
  kill_port 8080
  jl exec "$JL_MACHINE_ID" -- sh -lc "
    mkdir -p '$LOG_DIR'
    setsid '$CLIENT_DIR/inferoute-client' --config '$CLIENT_DIR/$config' \
      > '$LOG_DIR/inferoute-client.log' 2>&1 </dev/null &
    echo 'client launched ($config)'
  "
  wait_gate "[$label] inferoute-client" "$CLIENT_WAIT_SEC" client_ready \
    || { jl exec "$JL_MACHINE_ID" -- tail -40 "$LOG_DIR/inferoute-client.log"; die "[$label] client not ready"; }

  step "[$label] client health"
  local health
  health=$(jl exec "$JL_MACHINE_ID" -- curl -s http://127.0.0.1:8080/api/health)
  printf '%s' "$health" | jq '{provider_type, tunnel: .cloudflare.url, models: [.data[]? | {id, verification_status}]}' || true
  TUNNEL=$(printf '%s' "$health" | jq -r '.cloudflare.url // empty')

  step "[$label] provider health (DB)"
  wait_provider_green

  step "[$label] inference tests (alias=$alias)"
  if SKIP_WAIT=1 MODEL_ALIAS="$alias" bash "$SCRIPT_DIR/references/test-inference.sh"; then
    log "[$label] TESTS PASSED"
  else
    OVERALL=1
    warn "[$label] TESTS FAILED"
  fi
}

# ── 1. ngrok ────────────────────────────────────────────────────────────────
step "ngrok"
tunnel_online() { curl -sf http://127.0.0.1:4040/api/tunnels 2>/dev/null | grep -q "$INFEROUTE_PLATFORM_URL"; }
if tunnel_online; then
  log "ngrok already online (reusing): $INFEROUTE_PLATFORM_URL"
else
  pkill -f "ngrok http" 2>/dev/null || true
  sleep 1
  log "starting ngrok -> $NGROK_LOG"
  nohup sh -c "$NGROK_CMD --log=stdout" >"$NGROK_LOG" 2>&1 &
  STARTED_NGROK=1
  for _ in $(seq 1 30); do tunnel_online && break; sleep 1; done
  tunnel_online || { tail -20 "$NGROK_LOG" >&2; die "ngrok did not come online"; }
fi
log "ngrok tunnel started: $INFEROUTE_PLATFORM_URL"

# Pause always runs from here on.
trap finish EXIT INT TERM

# ── 2. resume JarvisLab (handles instance-id rotation) ───────────────────────
step "resume JarvisLab $JL_MACHINE_ID"
resume_out=$(jl resume "$JL_MACHINE_ID" --yes ${JL_GPU:+--gpu "$JL_GPU"} 2>&1 || true)
printf '%s\n' "$resume_out"
new_id=$(printf '%s' "$resume_out" | grep -i "changed" | grep -oE '[0-9]{4,}' | tail -1 || true)
if [ -n "$new_id" ] && [ "$new_id" != "$JL_MACHINE_ID" ]; then
  log "instance id rotated: $JL_MACHINE_ID -> $new_id"
  JL_MACHINE_ID="$new_id"
fi

log "waiting for Running (max ${RUNNING_WAIT_SEC}s)"
start=$SECONDS
until [ "$(jl get "$JL_MACHINE_ID" --json 2>/dev/null | jq -r '.status // empty')" = "Running" ]; do
  (( SECONDS - start >= RUNNING_WAIT_SEC )) && die "instance not Running after ${RUNNING_WAIT_SEC}s"
  sleep 10; echo "  ...waiting Running"
done
jl get "$JL_MACHINE_ID" --json | jq '{status, gpu, ssh_command}'

log "waiting for SSH (max ${SSH_WAIT_SEC}s)"
start=$SECONDS
until jl exec "$JL_MACHINE_ID" -- true >/dev/null 2>&1; do
  (( SECONDS - start >= SSH_WAIT_SEC )) && die "SSH not reachable after ${SSH_WAIT_SEC}s"
  sleep 10; echo "  ...waiting SSH"
done
jl exec "$JL_MACHINE_ID" -- nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader || true

# ── 3. verify GPU can reach the tunnel ───────────────────────────────────────
step "GPU -> platform reachability"
PROBE_URL="${INFEROUTE_PLATFORM_URL%/}/api/models/approved-builds"
gcode=$(jl exec "$JL_MACHINE_ID" -- curl -4 -s -o /dev/null -w '%{http_code}' --max-time 15 "$PROBE_URL" 2>/dev/null || true)
[ "$gcode" = "200" ] || die "JarvisLab cannot reach $PROBE_URL (http=${gcode:-000}) — is ngrok + inferoute-node up?"
log "GPU -> platform OK (http=$gcode)"

# ── 4. sync latest source from GitHub + build so every e2e runs the newest binary
# The JL client dir is a deployment, not a dev tree — hard-reset to the upstream
# branch so we always test exactly what's on GitHub. config.yaml is git-ignored,
# so it survives the reset. Set CLIENT_GIT_PULL=0 to build the current checkout.
step "sync + build inferoute-client (branch: $CLIENT_GIT_BRANCH)"
jl exec "$JL_MACHINE_ID" -- sh -lc "
  set -e
  # jl exec's non-interactive shell doesn't source the profile that adds Go to
  # PATH, so build.sh fails with 'go: command not found'. Add the usual install
  # locations (override with GO_BIN_DIR in .env if go lives elsewhere).
  export PATH=\"\$PATH:${GO_BIN_DIR:-/usr/local/go/bin}:\$HOME/go/bin:/usr/lib/go/bin:/snap/bin\"
  command -v go >/dev/null || { echo '[build] go not found on PATH — set GO_BIN_DIR in references/.env'; exit 127; }
  echo \"[build] using \$(go version) at \$(command -v go)\"
  cd '$CLIENT_DIR'
  if [ '$CLIENT_GIT_PULL' = '1' ]; then
    echo '[build] fetching origin/$CLIENT_GIT_BRANCH'
    git fetch --prune origin '$CLIENT_GIT_BRANCH'
    git checkout '$CLIENT_GIT_BRANCH'
    git reset --hard 'origin/$CLIENT_GIT_BRANCH'
    echo \"[build] now at \$(git rev-parse --short HEAD) — \$(git log -1 --pretty=%s)\"
  else
    echo '[build] CLIENT_GIT_PULL=0 — building current checkout'
  fi
  bash scripts/build.sh
"

# ── 5. vLLM phase ────────────────────────────────────────────────────────────
if [ "$RUN_VLLM" = "1" ]; then
  # Start vLLM and wait until it serves BEFORE touching the client. The client
  # marks itself red at startup if no models are up, recovering only on its next
  # heartbeat — so the backend must be ready first.
  step "[vLLM] start vLLM ($VLLM_MODEL)"
  jl exec "$JL_MACHINE_ID" -- sh -lc "
    mkdir -p '$LOG_DIR'
    if curl -sf http://127.0.0.1:8000/v1/models >/dev/null 2>&1; then
      echo 'vllm already serving'
    else
      setsid '$VLLM_BIN' serve '$VLLM_MODEL' --host 0.0.0.0 --port 8000 \
        > '$LOG_DIR/vllm.log' 2>&1 </dev/null &
      echo 'vllm launched'
    fi
  "
  wait_gate "[vLLM] vLLM" "$VLLM_WAIT_SEC" vllm_ready \
    || { jl exec "$JL_MACHINE_ID" -- tail -40 "$LOG_DIR/vllm.log"; die "vLLM not ready"; }

  run_client_phase "vLLM" "$CLIENT_CONFIG" "$INFEROUTE_MODEL_ALIAS"
else
  warn "RUN_VLLM=0 — skipping vLLM phase"
fi

# ── 6. Ollama phase ──────────────────────────────────────────────────────────
if [ "$RUN_OLLAMA" = "1" ]; then
  # Free the GPU: vLLM must be down before Ollama loads the model.
  step "[Ollama] stop vLLM (free GPU)"
  kill_port 8000

  step "[Ollama] serve $OLLAMA_MODEL"
  jl exec "$JL_MACHINE_ID" -- sh -lc "
    mkdir -p '$LOG_DIR'
    if ! curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags >/dev/null 2>&1; then
      echo 'starting ollama serve'
      setsid ollama serve > '$LOG_DIR/ollama.log' 2>&1 </dev/null &
      for _ in \$(seq 1 30); do curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags >/dev/null 2>&1 && break; sleep 2; done
    else
      echo 'ollama already serving'
    fi
    echo 'pulling $OLLAMA_MODEL'
    ollama pull '$OLLAMA_MODEL'
  "
  wait_gate "[Ollama] model $OLLAMA_MODEL" "$VLLM_WAIT_SEC" ollama_ready \
    || { jl exec "$JL_MACHINE_ID" -- tail -40 "$LOG_DIR/ollama.log"; die "ollama model not ready"; }

  # Derive the ollama client config from the vLLM one so api_key/url are reused.
  step "[Ollama] generate $OLLAMA_CLIENT_CONFIG from $CLIENT_CONFIG"
  jl exec "$JL_MACHINE_ID" -- sh -lc "
    cd '$CLIENT_DIR'
    sed -e 's|provider_type:.*|provider_type: \"ollama\"|' \
        -e 's|llm_url:.*|llm_url: \"$OLLAMA_URL\"|' \
        '$CLIENT_CONFIG' > '$OLLAMA_CLIENT_CONFIG'
    echo '--- $OLLAMA_CLIENT_CONFIG (provider block) ---'
    grep -E 'provider_type|llm_url' '$OLLAMA_CLIENT_CONFIG'
  "

  run_client_phase "Ollama" "$OLLAMA_CLIENT_CONFIG" "$OLLAMA_MODEL_ALIAS"
else
  warn "RUN_OLLAMA=0 — skipping Ollama phase"
fi

exit "$OVERALL"
