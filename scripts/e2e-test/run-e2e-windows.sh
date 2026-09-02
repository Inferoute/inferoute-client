#!/usr/bin/env bash
# End-to-end inferoute-client test against the Windows GCE GPU box.
#
#   start VM -> wait for boot/SSH -> pull + rebuild client -> Ollama + config
#   -> inference tests (Mac consumer, same suite as run-e2e.sh) -> stop VM
#
# Native Windows is Ollama-only (no vLLM). Same references/.env as run-e2e-linux.sh.
# Mac Mini counterpart (Ollama only, Mini stays up): ./run-e2e-mac.sh
#
# The instance is ALWAYS stopped on exit (success, failure, or Ctrl-C) unless KEEP=1.
#
# Usage:
#   ./run-e2e-windows.sh              # start, test, stop
#   KEEP=1 ./run-e2e-windows.sh       # leave the VM running for debugging
#   ./run-e2e-windows.sh teardown     # just stop the VM and exit
#
# Config comes from references/.env (override path with E2E_ENV).
# references/.env is git-ignored — never commit filled secrets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/references/.env}"
# shellcheck source=/dev/null
[ -f "$E2E_ENV" ] && source "$E2E_ENV"

command -v gcloud >/dev/null || { echo "[e2e-win] gcloud not on PATH" >&2; exit 1; }
command -v jq >/dev/null || { echo "[e2e-win] jq not on PATH" >&2; exit 1; }

: "${INFEROUTE_PLATFORM_URL:?set INFEROUTE_PLATFORM_URL in references/.env}"
: "${INFEROUTE_CONSUMER_URL:?set INFEROUTE_CONSUMER_URL in references/.env}"
: "${CONSUMER_API_KEY:?set CONSUMER_API_KEY in references/.env}"
: "${PROVIDER_API_KEY:?set PROVIDER_API_KEY in references/.env}"

GCE_PROJECT="${GCE_PROJECT:-inferoute}"
GCE_ZONE="${GCE_ZONE:-us-central1-c}"
GCE_INSTANCE="${GCE_INSTANCE:-inferoute-client}"
GCE_EXTERNAL_IP="${GCE_EXTERNAL_IP:-35.238.4.74}"
WIN_SSH_USER="${WIN_SSH_USER:-charles_holtzkampf}"
WIN_HOME="${WIN_HOME:-C:/Users/${WIN_SSH_USER}}"
WIN_CLIENT_DIR="${WIN_CLIENT_DIR:-${WIN_HOME}/inferoute-client}"
WIN_GO_BIN_DIR="${WIN_GO_BIN_DIR:-C:/Program Files/Go/bin}"
WIN_LOG_DIR="${WIN_LOG_DIR:-${WIN_CLIENT_DIR}/logs}"
CLIENT_GIT_REPO="${CLIENT_GIT_REPO:-https://github.com/Inferoute/inferoute-client.git}"

CLIENT_CONFIG="${CLIENT_CONFIG:-config.yaml}"
OLLAMA_MODEL="${OLLAMA_MODEL:-qwen3:0.6b}"
OLLAMA_MODEL_ALIAS="${OLLAMA_MODEL_ALIAS:-gguf/qwen3:0.6b}"
OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
CLIENT_GIT_PULL="${CLIENT_GIT_PULL:-1}"
CLIENT_GIT_BRANCH="${CLIENT_GIT_BRANCH:-main}"

NGROK_LOG="${NGROK_LOG:-/tmp/inferoute-ngrok.log}"
NGROK_CMD="${NGROK_CMD:-ngrok http 80 --host-header=localhost --url=${INFEROUTE_PLATFORM_URL#https://}}"
CLIENT_WAIT_SEC="${CLIENT_WAIT_SEC:-180}"
RUNNING_WAIT_SEC="${RUNNING_WAIT_SEC:-360}"
SSH_WAIT_SEC="${SSH_WAIT_SEC:-600}"
PROVIDER_WAIT_SEC="${PROVIDER_WAIT_SEC:-180}"
DB_CONTAINER="${DB_CONTAINER:-cockroachdb}"
DB_NAME="${DB_NAME:-inferoute}"

KEEP="${KEEP:-0}"
STARTED_NGROK=0
OVERALL=0
TUNNEL=""
TMPDIR_WIN=""
SSH_OK=0

log()  { printf '\n\033[1;36m[e2e-win] %s\033[0m\n' "$*"; }
step() { printf '\033[1;35m[e2e-win] ── %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[e2e-win] %s\033[0m\n' "$*" >&2; }
die()  { printf '\033[1;31m[e2e-win] %s\033[0m\n' "$*" >&2; exit 1; }

gce() {
  gcloud --project="$GCE_PROJECT" --quiet "$@"
}

instance_status() {
  gce compute instances describe "$GCE_INSTANCE" --zone="$GCE_ZONE" --format='get(status)' 2>/dev/null || true
}

vm_running() { [ "$(instance_status)" = "RUNNING" ]; }
vm_stopped() {
  local st
  st=$(instance_status)
  [ "$st" = "TERMINATED" ] || [ "$st" = "STOPPED" ]
}

stop_vm() {
  local st
  st=$(instance_status)
  if [ "$st" = "TERMINATED" ] || [ "$st" = "STOPPED" ]; then
    log "VM already stopped ($st)"
    return 0
  fi
  log "Stopping GCE $GCE_INSTANCE (${st:-unknown}) in $GCE_ZONE"
  gce compute instances stop "$GCE_INSTANCE" --zone="$GCE_ZONE" \
    || warn "stop FAILED — run: gcloud compute instances stop $GCE_INSTANCE --zone=$GCE_ZONE --project=$GCE_PROJECT --quiet"
}

finish() {
  local code=$?
  trap - EXIT INT TERM
  [ -n "$TMPDIR_WIN" ] && rm -rf "$TMPDIR_WIN"
  wipe_remote_params
  if [ "$KEEP" = "1" ]; then
    warn "KEEP=1 — leaving $GCE_INSTANCE running (stop manually: $0 teardown)"
  else
    stop_vm
  fi
  [ "$STARTED_NGROK" = "1" ] && { log "Stopping ngrok (we started it)"; pkill -f "ngrok http" 2>/dev/null || true; }
  [ "$code" = "0" ] && log "DONE ✓" || warn "EXIT code $code"
  exit "$code"
}

if [ "${1:-}" = "teardown" ]; then
  log "Teardown: stopping $GCE_INSTANCE"
  stop_vm
  exit 0
fi

wait_gate() {
  local desc="$1" max="$2"; shift 2
  local start=$SECONDS
  until "$@"; do
    (( SECONDS - start >= max )) && { warn "TIMEOUT: $desc (>${max}s)"; return 1; }
    echo "  ...waiting $desc"; sleep 15
  done
  log "$desc ready"
}

# $1 = PowerShell expression. Avoid '%' — cmd.exe (OpenSSH default shell) expands %VAR%.
win_ps() {
  gce compute ssh "${WIN_SSH_USER}@${GCE_INSTANCE}" \
    --zone="$GCE_ZONE" \
    --strict-host-key-checking=no \
    --ssh-flag="-o ConnectTimeout=15" \
    --command="powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command $1"
}

win_ps_ok() { win_ps "$1" >/dev/null 2>&1; }

client_ready() { win_ps_ok "curl.exe -sf http://127.0.0.1:8080/api/health"; }

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

ps_literal() {
  local s=$1
  printf "%s" "${s//\'/\'\'}"
}

# PowerShell -File is happier with backslashes.
win_path() { printf '%s' "${1//\//\\}"; }

wipe_remote_params() {
  [ "$SSH_OK" = "1" ] || return 0
  win_ps "Remove-Item -LiteralPath '$(ps_literal "$WIN_HOME")/inferoute-e2e-params.ps1' -Force -ErrorAction SilentlyContinue" >/dev/null 2>&1 || true
}

# ── 1. ngrok (Windows client must reach the Mac platform via the reserved domain)
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

# Stop always runs from here on (including if start/SSH/tests fail).
trap finish EXIT INT TERM

# ── 2. start GCE Windows VM ──────────────────────────────────────────────────
step "start GCE $GCE_INSTANCE ($GCE_ZONE) ip=$GCE_EXTERNAL_IP"
# Guest agent + gcloud SSH need this; idempotent, survives stop/start.
gce compute instances add-metadata "$GCE_INSTANCE" --zone="$GCE_ZONE" \
  --metadata=enable-windows-ssh=TRUE
st=$(instance_status)
log "current status: ${st:-unknown}"
case "$st" in
  RUNNING) log "already RUNNING" ;;
  STOPPING)
    log "waiting for STOPPING -> TERMINATED before start"
    wait_gate "VM terminated" 180 vm_stopped
    gce compute instances start "$GCE_INSTANCE" --zone="$GCE_ZONE"
    ;;
  *)
    gce compute instances start "$GCE_INSTANCE" --zone="$GCE_ZONE"
    ;;
esac

log "waiting for RUNNING (max ${RUNNING_WAIT_SEC}s)"
wait_gate "GCE RUNNING" "$RUNNING_WAIT_SEC" vm_running \
  || die "instance not RUNNING after ${RUNNING_WAIT_SEC}s"

log "waiting for SSH as $WIN_SSH_USER (max ${SSH_WAIT_SEC}s) — Windows guest + OpenSSH can take several minutes after RUNNING"
wait_gate "Windows SSH" "$SSH_WAIT_SEC" win_ps_ok "Write-Output ready" \
  || die "SSH not reachable after ${SSH_WAIT_SEC}s — is OpenSSH Server installed on the VM?"
SSH_OK=1

win_ps "nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader" || warn "nvidia-smi failed (driver still loading?)"

# ── 3. GPU -> platform reachability (ngrok must be visible from GCE) ─────────
step "Windows -> platform reachability"
PROBE_URL="${INFEROUTE_PLATFORM_URL%/}/api/models/approved-builds"
gcode=$(win_ps "try { (Invoke-WebRequest -UseBasicParsing -TimeoutSec 15 -Uri '$PROBE_URL').StatusCode } catch { 0 }" | tr -d '\r' | tail -1 || true)
[ "$gcode" = "200" ] || die "Windows VM cannot reach $PROBE_URL (http=${gcode:-000}) — is ngrok + inferoute-node up?"
log "Windows -> platform OK (http=$gcode)"

# ── 4. copy remote setup + params, then pull/build/config/start ──────────────
step "sync + build + start (branch: $CLIENT_GIT_BRANCH, dir: $WIN_CLIENT_DIR)"
TMPDIR_WIN=$(mktemp -d)
PARAMS_FILE="$TMPDIR_WIN/inferoute-e2e-params.ps1"
cat >"$PARAMS_FILE" <<EOF
\$E2E = @{
  ClientDir      = '$(ps_literal "$WIN_CLIENT_DIR")'
  GitBranch      = '$(ps_literal "$CLIENT_GIT_BRANCH")'
  GitRepo        = '$(ps_literal "$CLIENT_GIT_REPO")'
  GitPull        = '$(ps_literal "$CLIENT_GIT_PULL")'
  ProviderApiKey = '$(ps_literal "$PROVIDER_API_KEY")'
  PlatformUrl    = '$(ps_literal "$INFEROUTE_PLATFORM_URL")'
  OllamaModel    = '$(ps_literal "$OLLAMA_MODEL")'
  OllamaUrl      = '$(ps_literal "$OLLAMA_URL")'
  ConfigFile     = '$(ps_literal "$CLIENT_CONFIG")'
  GoBinDir       = '$(ps_literal "$WIN_GO_BIN_DIR")'
  LogDir         = '$(ps_literal "$WIN_LOG_DIR")'
}
EOF

gce compute scp \
  "$SCRIPT_DIR/references/windows-remote-setup.ps1" \
  "${WIN_SSH_USER}@${GCE_INSTANCE}:inferoute-e2e-setup.ps1" \
  --zone="$GCE_ZONE" \
  --strict-host-key-checking=no

gce compute scp \
  "$PARAMS_FILE" \
  "${WIN_SSH_USER}@${GCE_INSTANCE}:inferoute-e2e-params.ps1" \
  --zone="$GCE_ZONE" \
  --strict-host-key-checking=no

rm -rf "$TMPDIR_WIN"
TMPDIR_WIN=""

log "running windows-remote-setup.ps1 on the VM"
SETUP_FILE="$(win_path "$WIN_HOME")\\inferoute-e2e-setup.ps1"
PARAMS_REMOTE="$(win_path "$WIN_HOME")\\inferoute-e2e-params.ps1"
gce compute ssh "${WIN_SSH_USER}@${GCE_INSTANCE}" \
  --zone="$GCE_ZONE" \
  --strict-host-key-checking=no \
  --ssh-flag="-o ConnectTimeout=15" \
  --command="powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ${SETUP_FILE} -ParamsFile ${PARAMS_REMOTE}" \
  || {
    warn "remote setup failed — last client log:"
    win_ps "if (Test-Path '${WIN_LOG_DIR}/inferoute-client.err.log') { Get-Content '${WIN_LOG_DIR}/inferoute-client.err.log' -Tail 40 }" || true
    wipe_remote_params
    die "windows-remote-setup.ps1 failed"
  }
wipe_remote_params

wait_gate "inferoute-client HTTP" "$CLIENT_WAIT_SEC" client_ready \
  || {
    warn "client HTTP timeout — process + last logs:"
    win_ps "Get-Process -Name inferoute-client,ollama,cloudflared -ErrorAction SilentlyContinue | Format-Table Id,ProcessName,StartTime -AutoSize; Write-Output '--- err ---'; if (Test-Path '${WIN_LOG_DIR}/inferoute-client.err.log') { Get-Content '${WIN_LOG_DIR}/inferoute-client.err.log' -Tail 50 }; Write-Output '--- log ---'; if (Test-Path '${WIN_LOG_DIR}/inferoute-client.log') { Get-Content '${WIN_LOG_DIR}/inferoute-client.log' -Tail 50 }" || true
    die "client not ready after setup"
  }

step "client health"
health=$(win_ps "curl.exe -s http://127.0.0.1:8080/api/health" | tr -d '\r')
printf '%s' "$health" | jq '{provider_type, tunnel: .cloudflare.url, models: [.data[]? | {id, verification_status}]}' || true
TUNNEL=$(printf '%s' "$health" | jq -r '.cloudflare.url // empty')

step "provider health (DB)"
wait_provider_green

step "inference tests (alias=$OLLAMA_MODEL_ALIAS)"
if SKIP_WAIT=1 MODEL_ALIAS="$OLLAMA_MODEL_ALIAS" bash "$SCRIPT_DIR/references/test-inference.sh"; then
  log "TESTS PASSED"
else
  OVERALL=1
  warn "TESTS FAILED"
fi

exit "$OVERALL"
