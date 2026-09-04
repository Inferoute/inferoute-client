#!/usr/bin/env bash
# End-to-end inferoute-client test against a LAN Mac Mini.
#
#   wait for SSH -> pull + rebuild client -> Ollama + config
#   -> inference tests (same suite as run-e2e-linux.sh) -> stop ollama + client
#
# Native Mac is Ollama-only (no vLLM). Same references/.env as the other runners.
#
# The Mac Mini itself is NEVER stopped/slept. On exit we only kill ollama,
# inferoute-client, and leftover cloudflared — unless KEEP=1.
#
# Usage:
#   ./run-e2e-mac.sh              # test, then stop ollama + client
#   KEEP=1 ./run-e2e-mac.sh       # leave ollama + client running
#   SKIP_TESTS=1 KEEP=1 ./run-e2e-mac.sh  # bring up, skip inference suite (used by run-cluster.sh)
#   ./run-e2e-mac.sh teardown     # just stop ollama + client and exit
#
# Config comes from references/.env (override path with E2E_ENV).
# references/.env is git-ignored — never commit filled secrets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/references/.env}"
# shellcheck source=/dev/null
[ -f "$E2E_ENV" ] && source "$E2E_ENV"

command -v jq >/dev/null || { echo "[e2e-mac] jq not on PATH" >&2; exit 1; }
command -v ssh >/dev/null || { echo "[e2e-mac] ssh not on PATH" >&2; exit 1; }
# Password SSH from a TTY needs sshpass — OpenSSH ASKPASS is unreliable on macOS.
command -v sshpass >/dev/null || {
  echo "[e2e-mac] sshpass not on PATH (needed for MAC_SSH_PASSWORD)." >&2
  echo "  brew install hudochenkov/sshpass/sshpass" >&2
  exit 1
}

: "${INFEROUTE_PLATFORM_URL:?set INFEROUTE_PLATFORM_URL in references/.env}"
: "${INFEROUTE_CONSUMER_URL:?set INFEROUTE_CONSUMER_URL in references/.env}"
: "${CONSUMER_API_KEY:?set CONSUMER_API_KEY in references/.env}"
: "${PROVIDER_API_KEY:?set PROVIDER_API_KEY in references/.env}"
: "${MAC_SSH_USER:?set MAC_SSH_USER in references/.env}"
: "${MAC_SSH_PASSWORD:?set MAC_SSH_PASSWORD in references/.env}"

MAC_SSH_HOST="${MAC_SSH_HOST:-192.168.110.124}"
MAC_HOME="${MAC_HOME:-/Users/${MAC_SSH_USER}}"
MAC_CLIENT_DIR="${MAC_CLIENT_DIR:-${MAC_HOME}/inferoute-client}"
MAC_GO_BIN_DIR="${MAC_GO_BIN_DIR:-/opt/homebrew/bin}"
MAC_LOG_DIR="${MAC_LOG_DIR:-${MAC_CLIENT_DIR}/logs}"
CLIENT_GIT_REPO="${CLIENT_GIT_REPO:-https://github.com/Inferoute/inferoute-client.git}"

CLIENT_CONFIG="${CLIENT_CONFIG:-config.yaml}"
OLLAMA_MODEL="${OLLAMA_MODEL:-qwen3:0.6b}"
OLLAMA_MODEL_ALIAS="${OLLAMA_MODEL_ALIAS:-gguf/qwen3:0.6b}"
OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
OLLAMA_PORT="${OLLAMA_PORT:-11434}"
CLIENT_GIT_PULL="${CLIENT_GIT_PULL:-1}"
CLIENT_GIT_BRANCH="${CLIENT_GIT_BRANCH:-main}"

NGROK_LOG="${NGROK_LOG:-/tmp/inferoute-ngrok.log}"
NGROK_CMD="${NGROK_CMD:-ngrok http 80 --host-header=localhost --url=${INFEROUTE_PLATFORM_URL#https://}}"
CLIENT_WAIT_SEC="${CLIENT_WAIT_SEC:-180}"
SSH_WAIT_SEC="${SSH_WAIT_SEC:-60}"
PROVIDER_WAIT_SEC="${PROVIDER_WAIT_SEC:-180}"
OLLAMA_WAIT_SEC="${OLLAMA_WAIT_SEC:-600}"
DB_CONTAINER="${DB_CONTAINER:-cockroachdb}"
DB_NAME="${DB_NAME:-inferoute}"

KEEP="${KEEP:-0}"
STARTED_NGROK=0
OVERALL=0
TUNNEL=""
SSH_OK=0
TMPDIR_MAC=""

log()  { printf '\n\033[1;36m[e2e-mac] %s\033[0m\n' "$*"; }
step() { printf '\033[1;35m[e2e-mac] ── %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[e2e-mac] %s\033[0m\n' "$*" >&2; }
die()  { printf '\033[1;31m[e2e-mac] %s\033[0m\n' "$*" >&2; exit 1; }

setup_ssh() {
  TMPDIR_MAC=$(mktemp -d)
  export SSHPASS="$MAC_SSH_PASSWORD"
}

mac_ssh() {
  local opts=(
    -o StrictHostKeyChecking=accept-new
    -o PreferredAuthentications=password,keyboard-interactive
    -o PubkeyAuthentication=no
    -o NumberOfPasswordPrompts=1
    -o ConnectTimeout=15
    -o ServerAliveInterval=15
    -o ControlMaster=auto
    -o ControlPersist=120
    -o "ControlPath=${TMPDIR_MAC}/cm.sock"
  )
  sshpass -e ssh "${opts[@]}" "${MAC_SSH_USER}@${MAC_SSH_HOST}" "$@"
}

# Non-interactive SSH does not load zsh/homebrew. Prepend the usual install dirs.
mac_sh() {
  local script=$1
  local path_prefix="${MAC_GO_BIN_DIR}:/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:\$HOME/go/bin:\$HOME/bin"
  mac_ssh bash -c "$(printf '%q' "set -euo pipefail
export PATH=\"${path_prefix}:\$PATH\"
${script}")"
}

mac_ok() { mac_sh "$1" >/dev/null 2>&1; }

wait_gate() {
  local desc="$1" max="$2"; shift 2
  local start=$SECONDS
  until "$@"; do
    (( SECONDS - start >= max )) && { warn "TIMEOUT: $desc (>${max}s)"; return 1; }
    echo "  ...waiting $desc"; sleep 15
  done
  log "$desc ready"
}

kill_port() {
  local port="$1"
  mac_sh "
    pids=\$(lsof -nP -iTCP:$port -sTCP:LISTEN -t 2>/dev/null || true)
    [ -n \"\$pids\" ] && kill \$pids 2>/dev/null || true
    sleep 2
    pids=\$(lsof -nP -iTCP:$port -sTCP:LISTEN -t 2>/dev/null || true)
    [ -n \"\$pids\" ] && kill -9 \$pids 2>/dev/null || true
    true
  "
}

# Stop provider processes only — never sleep/shutdown the Mini.
stop_services() {
  [ "$SSH_OK" = "1" ] || return 0
  log "Stopping inferoute-client + ollama on $MAC_SSH_HOST (Mini stays up)"
  mac_sh "
    osascript -e 'tell application \"Ollama\" to quit' >/dev/null 2>&1 || true
    sleep 1
    killall Ollama ollama inferoute-client cloudflared 2>/dev/null || true
    sleep 2
    killall -9 Ollama ollama inferoute-client cloudflared 2>/dev/null || true
    for p in 8080 $OLLAMA_PORT; do
      pids=\$(lsof -nP -iTCP:\$p -sTCP:LISTEN -t 2>/dev/null || true)
      [ -n \"\$pids\" ] && kill -9 \$pids 2>/dev/null || true
    done
    true
  " || warn "stop_services SSH failed — kill ollama / inferoute-client on the Mini by hand"
}

close_ssh() {
  [ -n "$TMPDIR_MAC" ] || return 0
  if [ -S "$TMPDIR_MAC/cm.sock" ]; then
    ssh -o ControlPath="$TMPDIR_MAC/cm.sock" -O exit "${MAC_SSH_USER}@${MAC_SSH_HOST}" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMPDIR_MAC"
  TMPDIR_MAC=""
}

client_ready() { mac_ok "curl -sf http://127.0.0.1:8080/api/health"; }
ollama_ready() { mac_ok "curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags 2>/dev/null | grep -q '$OLLAMA_MODEL'"; }
ssh_ready()    { mac_ok "true"; }

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

finish() {
  local code=$?
  trap - EXIT INT TERM
  if [ "$KEEP" = "1" ]; then
    warn "KEEP=1 — leaving ollama + inferoute-client running on $MAC_SSH_HOST (stop with: $0 teardown)"
  else
    stop_services
  fi
  close_ssh
  [ "$STARTED_NGROK" = "1" ] && { log "Stopping ngrok (we started it)"; pkill -f "ngrok http" 2>/dev/null || true; }
  [ "$code" = "0" ] && log "DONE ✓" || warn "EXIT code $code"
  exit "$code"
}

if [ "${1:-}" = "teardown" ]; then
  setup_ssh
  log "Teardown: stopping ollama + inferoute-client on $MAC_SSH_HOST"
  wait_gate "Mac Mini SSH" 30 ssh_ready || die "SSH not reachable — is Remote Login on?"
  SSH_OK=1
  stop_services
  close_ssh
  exit 0
fi

# ── 1. ngrok (Mini client must reach the Mac platform via the reserved domain)
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

setup_ssh
trap finish EXIT INT TERM

# ── 2. SSH ───────────────────────────────────────────────────────────────────
step "SSH $MAC_SSH_USER@$MAC_SSH_HOST"
wait_gate "Mac Mini SSH" "$SSH_WAIT_SEC" ssh_ready \
  || die "SSH not reachable after ${SSH_WAIT_SEC}s — enable Remote Login + password auth on the Mini"
SSH_OK=1
mac_sh "echo \"\$(scutil --get ComputerName 2>/dev/null || hostname)  \$(uname -m)  \$(sw_vers -productVersion 2>/dev/null || true)\"" || true

# ── 3. Mini -> platform reachability ─────────────────────────────────────────
step "Mac Mini -> platform reachability"
PROBE_URL="${INFEROUTE_PLATFORM_URL%/}/api/models/approved-builds"
gcode=$(mac_sh "curl -4 -s -o /dev/null -w '%{http_code}' --max-time 15 '$PROBE_URL'" || true)
[ "$gcode" = "200" ] || die "Mac Mini cannot reach $PROBE_URL (http=${gcode:-000}) — is ngrok + inferoute-node up?"
log "Mac Mini -> platform OK (http=$gcode)"

# ── 4. tools, clone/sync, build, config, ollama, client ──────────────────────
step "sync + build inferoute-client (branch: $CLIENT_GIT_BRANCH, dir: $MAC_CLIENT_DIR)"
mac_sh "
  for tool in git go ollama curl; do
    command -v \$tool >/dev/null || { echo \"\$tool not found on PATH — install it on the Mini\"; exit 127; }
  done
  echo \"[build] \$(go version) at \$(command -v go)\"
  echo \"[build] ollama=\$(command -v ollama)  git=\$(git --version)\"

  if ! command -v cloudflared >/dev/null; then
    echo '[build] cloudflared not on PATH — downloading to \$HOME/bin'
    mkdir -p \"\$HOME/bin\"
    case \$(uname -m) in
      arm64) url='https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-darwin-arm64.tgz' ;;
      *)     url='https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-darwin-amd64.tgz' ;;
    esac
    curl -fsSL \"\$url\" -o /tmp/cloudflared.tgz
    tar -xzf /tmp/cloudflared.tgz -C /tmp
    mv /tmp/cloudflared \"\$HOME/bin/cloudflared\"
    chmod +x \"\$HOME/bin/cloudflared\"
    rm -f /tmp/cloudflared.tgz
  fi
  echo \"[build] cloudflared=\$(command -v cloudflared)\"

  if [ ! -d '$MAC_CLIENT_DIR/.git' ]; then
    if [ -e '$MAC_CLIENT_DIR' ]; then
      echo '$MAC_CLIENT_DIR exists but is not a git checkout'
      exit 1
    fi
    echo '[build] cloning $CLIENT_GIT_REPO -> $MAC_CLIENT_DIR'
    git clone --branch '$CLIENT_GIT_BRANCH' '$CLIENT_GIT_REPO' '$MAC_CLIENT_DIR'
  fi

  cd '$MAC_CLIENT_DIR'
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

step "write $CLIENT_CONFIG (provider_type=ollama)"
mac_sh "
  set -e
  cd '$MAC_CLIENT_DIR'
  [ -f config.yaml.example ] || { echo 'config.yaml.example missing'; exit 1; }
  sed -e 's|^[[:space:]]*api_key:.*|  api_key: \"$PROVIDER_API_KEY\"|' \
      -e 's|^[[:space:]]*url:.*|  url: \"$INFEROUTE_PLATFORM_URL\"|' \
      -e 's|^[[:space:]]*provider_type:.*|  provider_type: \"ollama\"|' \
      -e 's|^[[:space:]]*llm_url:.*|  llm_url: \"$OLLAMA_URL\"|' \
      config.yaml.example > '$CLIENT_CONFIG'
  echo '--- $CLIENT_CONFIG (provider block) ---'
  grep -E 'url:|provider_type:|llm_url:' '$CLIENT_CONFIG'
"

step "[Ollama] serve $OLLAMA_MODEL"
mac_sh "
  mkdir -p '$MAC_LOG_DIR'
  if ! curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags >/dev/null 2>&1; then
    echo 'starting ollama serve'
    nohup ollama serve > '$MAC_LOG_DIR/ollama.log' 2>&1 </dev/null &
    sleep 1
    for _ in \$(seq 1 30); do curl -sf http://127.0.0.1:$OLLAMA_PORT/api/tags >/dev/null 2>&1 && break; sleep 2; done
  else
    echo 'ollama already serving'
  fi
  echo 'pulling $OLLAMA_MODEL'
  ollama pull '$OLLAMA_MODEL'
"
wait_gate "[Ollama] model $OLLAMA_MODEL" "$OLLAMA_WAIT_SEC" ollama_ready \
  || { mac_sh "tail -40 '$MAC_LOG_DIR/ollama.log'" || true; die "ollama model not ready"; }

step "[Ollama] (re)start inferoute-client with $CLIENT_CONFIG"
kill_port 8080
mac_sh "
  pkill -x inferoute-client 2>/dev/null || true
  mkdir -p '$MAC_LOG_DIR'
  nohup '$MAC_CLIENT_DIR/inferoute-client' --config '$MAC_CLIENT_DIR/$CLIENT_CONFIG' \
    > '$MAC_LOG_DIR/inferoute-client.log' 2>&1 </dev/null &
  sleep 1
  echo 'client launched ($CLIENT_CONFIG)'
"
wait_gate "inferoute-client HTTP" "$CLIENT_WAIT_SEC" client_ready \
  || {
    warn "client HTTP timeout — last logs:"
    mac_sh "tail -50 '$MAC_LOG_DIR/inferoute-client.log'" || true
    die "client not ready after setup"
  }

step "client health"
health=$(mac_sh "curl -s http://127.0.0.1:8080/api/health")
printf '%s' "$health" | jq '{provider_type, tunnel: .cloudflare.url, models: [.data[]? | {id, verification_status}]}' || true
TUNNEL=$(printf '%s' "$health" | jq -r '.cloudflare.url // empty')

step "provider health (DB)"
wait_provider_green

if [ "${SKIP_TESTS:-0}" = "1" ]; then
  log "SKIP_TESTS=1 — leaving ollama + client up (no inference suite)"
else
  step "inference tests (alias=$OLLAMA_MODEL_ALIAS)"
  if SKIP_WAIT=1 MODEL_ALIAS="$OLLAMA_MODEL_ALIAS" bash "$SCRIPT_DIR/references/test-inference.sh"; then
    log "TESTS PASSED"
  else
    OVERALL=1
    warn "TESTS FAILED"
  fi
fi

exit "$OVERALL"
