#!/usr/bin/env bash
# Bring up provider(s) for local inference tests.
#
# Cluster (default):
#   ngrok (once) -> Linux + Windows + Mac Mini, all Ollama, same model
#   -> hold until Y or Ctrl-C -> pause Windows GCE + JarvisLab
#
# Linux-only mode (./run-cluster.sh linux):
#   JarvisLab only — H100/H200 GPU, vLLM + Qwen2.5-Coder-32B by default.
#   Override with JL_GPU / VLLM_MODEL in .env or on the command line.
#
# Mac Mini is NEVER slept/stopped. Its ollama + client stay up after teardown
# (re-run this script to refresh them; ./run-e2e-mac.sh teardown to kill them).
#
# Each machine gets its own provider API key. CONSUMER_API_KEY is shared.
# Inference is NOT run here — hit the consumer yourself while this holds.
#
# Usage:
#   ./run-cluster.sh              # start all 3, hold, pause Win+Linux on Y/Ctrl-C
#   ./run-cluster.sh linux        # JarvisLab only (H100/H200, vLLM Qwen2.5-Coder-32B)
#   KEEP=1 ./run-cluster.sh       # hold, but leave Win+Linux running on exit
#   RUN_WINDOWS=0 ./run-cluster.sh
#   ./run-cluster.sh teardown     # pause Win+Linux (Mini stays up)
#   ./run-cluster.sh linux teardown  # pause JarvisLab only
#
# Config: references/.env (override with E2E_ENV).
#   Cluster: PROVIDER_API_KEY_LINUX / _WINDOWS / _MAC must be set and unique.
#   Linux-only: PROVIDER_API_KEY_LINUX is enough.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/references/.env}"
# shellcheck source=/dev/null
[ -f "$E2E_ENV" ] && source "$E2E_ENV"

MODE="cluster"
if [ "${1:-}" = "linux" ]; then
  MODE="linux"
  shift
  RUN_WINDOWS=0
  RUN_MAC=0
fi

NGROK_LOG="${NGROK_LOG:-/tmp/inferoute-ngrok.log}"
NGROK_CMD="${NGROK_CMD:-ngrok http 80 --host-header=localhost --url=${INFEROUTE_PLATFORM_URL#https://}}"

if [ "$MODE" = "linux" ]; then
  VLLM_MODEL="${VLLM_MODEL:-Qwen/Qwen2.5-Coder-32B-Instruct}"
  INFEROUTE_MODEL_ALIAS="${INFEROUTE_MODEL_ALIAS:-Qwen/Qwen2.5-Coder-32B-Instruct}"
  JL_GPU="${JL_GPU:-H100}"
  JL_GPU_FALLBACK="${JL_GPU_FALLBACK:-H100,H200}"
else
  OLLAMA_MODEL="${OLLAMA_MODEL:-qwen2.5-coder:7b-instruct}"
  OLLAMA_MODEL_ALIAS="${OLLAMA_MODEL_ALIAS:-gguf/${OLLAMA_MODEL}}"
fi

RUN_LINUX="${RUN_LINUX:-1}"
RUN_WINDOWS="${RUN_WINDOWS:-1}"
RUN_MAC="${RUN_MAC:-1}"
KEEP="${KEEP:-0}"
STARTED_NGROK=0
STARTED_LINUX=0
STARTED_WINDOWS=0
STARTED_MAC=0
CLUSTER_ENV_DIR=""
pids=()
names=()

log()  { printf '\n\033[1;36m[cluster] %s\033[0m\n' "$*"; }
step() { printf '\033[1;35m[cluster] ── %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[cluster] %s\033[0m\n' "$*" >&2; }
die()  { printf '\033[1;31m[cluster] %s\033[0m\n' "$*" >&2; exit 1; }

require_unique_keys() {
  local a=$1 b=$2 c=$3
  [ "$a" != "$b" ] && [ "$a" != "$c" ] && [ "$b" != "$c" ] \
    || die "PROVIDER_API_KEY_{LINUX,WINDOWS,MAC} must be 3 distinct provider keys"
}

pause_linux() {
  [ "$STARTED_LINUX" = "1" ] || [ "${1:-}" = "force" ] || return 0
  log "Pausing JarvisLab (run-e2e-linux.sh teardown)"
  bash "$SCRIPT_DIR/run-e2e-linux.sh" teardown \
    || warn "linux pause FAILED — run: $SCRIPT_DIR/run-e2e-linux.sh teardown"
}

stop_windows() {
  [ "$STARTED_WINDOWS" = "1" ] || [ "${1:-}" = "force" ] || return 0
  log "Stopping Windows GCE (run-e2e-windows.sh teardown)"
  bash "$SCRIPT_DIR/run-e2e-windows.sh" teardown \
    || warn "windows stop FAILED — run: $SCRIPT_DIR/run-e2e-windows.sh teardown"
}

# Background runners ignore SIGINT (no job control). Own PG so we can kill -$pid.
stop_children() {
  local pid alive=0
  [ "${#pids[@]}" -eq 0 ] && return 0
  for pid in "${pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      alive=1
      kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  [ "$alive" = "0" ] && { pids=(); return 0; }
  sleep 1
  for pid in "${pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
    fi
    wait "$pid" 2>/dev/null || true
  done
  pids=()
}

finish() {
  local code=$?
  trap - EXIT INT TERM
  stop_children
  [ -n "$CLUSTER_ENV_DIR" ] && rm -rf "$CLUSTER_ENV_DIR"
  if [ "$KEEP" = "1" ]; then
    if [ "$MODE" = "linux" ]; then
      warn "KEEP=1 — leaving JarvisLab running (pause with: $0 linux teardown)"
    else
      warn "KEEP=1 — leaving Windows + Linux running (pause with: $0 teardown)"
      warn "Mac Mini ollama + client left running"
    fi
  else
    [ "$RUN_LINUX" = "1" ] && pause_linux
    [ "$RUN_WINDOWS" = "1" ] && stop_windows
    if [ "$MODE" = "linux" ]; then
      log "JarvisLab paused"
    else
      log "Mac Mini left running (ollama + client stay up)"
    fi
  fi
  [ "$code" = "0" ] && log "DONE ✓" || warn "EXIT code $code"
  exit "$code"
}

if [ "${1:-}" = "teardown" ]; then
  if [ "$MODE" = "linux" ]; then
    log "Teardown: pause JarvisLab."
    STARTED_LINUX=1
    pause_linux force
  else
    log "Teardown: pause Windows + Linux. Mini stays up."
    STARTED_LINUX=1
    STARTED_WINDOWS=1
    pause_linux force
    stop_windows force
  fi
  exit 0
fi

: "${INFEROUTE_PLATFORM_URL:?set INFEROUTE_PLATFORM_URL in references/.env}"
: "${INFEROUTE_CONSUMER_URL:?set INFEROUTE_CONSUMER_URL in references/.env}"
: "${CONSUMER_API_KEY:?set CONSUMER_API_KEY in references/.env}"

if [ "$RUN_LINUX" = "1" ]; then
  : "${PROVIDER_API_KEY_LINUX:?set PROVIDER_API_KEY_LINUX in references/.env}"
fi
if [ "$RUN_WINDOWS" = "1" ]; then
  : "${PROVIDER_API_KEY_WINDOWS:?set PROVIDER_API_KEY_WINDOWS in references/.env}"
fi
if [ "$RUN_MAC" = "1" ]; then
  : "${PROVIDER_API_KEY_MAC:?set PROVIDER_API_KEY_MAC in references/.env}"
fi

if [ "$RUN_LINUX" = "1" ] && [ "$RUN_WINDOWS" = "1" ] && [ "$RUN_MAC" = "1" ]; then
  require_unique_keys "$PROVIDER_API_KEY_LINUX" "$PROVIDER_API_KEY_WINDOWS" "$PROVIDER_API_KEY_MAC"
fi

# ── ngrok first so the per-machine runners reuse it and do not kill it on exit
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
log "ngrok tunnel: $INFEROUTE_PLATFORM_URL (left running on exit — Mini stays a provider)"

trap finish EXIT INT TERM

# Child scripts `source` .env, which would overwrite PROVIDER_API_KEY / RUN_*.
# Append overrides so the sourced file wins in the direction we want.
CLUSTER_ENV_DIR=$(mktemp -d)
chmod 700 "$CLUSTER_ENV_DIR"
write_child_env() {
  local dest=$1
  shift
  cp "$E2E_ENV" "$dest"
  {
    echo
    echo "# cluster overrides (must come after the copied .env)"
    printf 'export KEEP=%q\n' "1"
    printf 'export SKIP_TESTS=%q\n' "1"
    while [ $# -gt 0 ]; do
      printf 'export %s=%q\n' "${1%%=*}" "${1#*=}"
      shift
    done
  } >> "$dest"
}

# ── start providers in parallel (KEEP=1 so their own EXIT traps do not pause)
if [ "$MODE" = "linux" ]; then
  step "start JarvisLab (vLLM, gpu=$JL_GPU fallback=$JL_GPU_FALLBACK, model=$VLLM_MODEL)"
else
  step "start providers (all Ollama, model=$OLLAMA_MODEL alias=$OLLAMA_MODEL_ALIAS)"
fi
launch() {
  local name=$1 envfile=$2 script=$3
  # New session so sleep/jl sit in this PG. finish() kills -$pid on Ctrl-C.
  E2E_ENV="$envfile" python3 -c 'import os, sys; os.setsid(); os.execvp(sys.argv[1], sys.argv[1:])' \
    bash "$script" &
  pids+=("$!")
  names+=("$name")
}

if [ "$RUN_LINUX" = "1" ]; then
  STARTED_LINUX=1
  if [ "$MODE" = "linux" ]; then
    linux_overrides=(
      "PROVIDER_API_KEY=$PROVIDER_API_KEY_LINUX"
      "RUN_VLLM=1"
      "RUN_OLLAMA=0"
      "VLLM_MODEL=$VLLM_MODEL"
      "INFEROUTE_MODEL_ALIAS=$INFEROUTE_MODEL_ALIAS"
    )
  else
    linux_overrides=(
      "PROVIDER_API_KEY=$PROVIDER_API_KEY_LINUX"
      "RUN_VLLM=0"
      "RUN_OLLAMA=1"
      "OLLAMA_MODEL=$OLLAMA_MODEL"
      "OLLAMA_MODEL_ALIAS=$OLLAMA_MODEL_ALIAS"
    )
  fi
  [ -n "${JL_GPU:-}" ] && linux_overrides+=("JL_GPU=$JL_GPU")
  [ -n "${JL_GPU_FALLBACK:-}" ] && linux_overrides+=("JL_GPU_FALLBACK=$JL_GPU_FALLBACK")
  write_child_env "$CLUSTER_ENV_DIR/linux.env" "${linux_overrides[@]}"
  launch linux "$CLUSTER_ENV_DIR/linux.env" "$SCRIPT_DIR/run-e2e-linux.sh"
else
  warn "RUN_LINUX=0 — skip JarvisLab"
fi

if [ "$RUN_WINDOWS" = "1" ]; then
  STARTED_WINDOWS=1
  write_child_env "$CLUSTER_ENV_DIR/windows.env" \
    "PROVIDER_API_KEY=$PROVIDER_API_KEY_WINDOWS" \
    "OLLAMA_MODEL=$OLLAMA_MODEL" \
    "OLLAMA_MODEL_ALIAS=$OLLAMA_MODEL_ALIAS"
  launch windows "$CLUSTER_ENV_DIR/windows.env" "$SCRIPT_DIR/run-e2e-windows.sh"
else
  warn "RUN_WINDOWS=0 — skip GCE"
fi

if [ "$RUN_MAC" = "1" ]; then
  STARTED_MAC=1
  write_child_env "$CLUSTER_ENV_DIR/mac.env" \
    "PROVIDER_API_KEY=$PROVIDER_API_KEY_MAC" \
    "OLLAMA_MODEL=$OLLAMA_MODEL" \
    "OLLAMA_MODEL_ALIAS=$OLLAMA_MODEL_ALIAS"
  launch mac "$CLUSTER_ENV_DIR/mac.env" "$SCRIPT_DIR/run-e2e-mac.sh"
else
  warn "RUN_MAC=0 — skip Mini"
fi

[ "${#pids[@]}" -gt 0 ] || die "nothing to start (all RUN_*=0)"

fail=0
for i in "${!pids[@]}"; do
  st=0
  wait "${pids[$i]}" || st=$?
  if [ "$st" = "0" ]; then
    log "${names[$i]} ready"
  else
    warn "${names[$i]} FAILED (exit $st)"
    fail=1
  fi
done
if [ "$fail" != "0" ]; then
  warn "partial cluster — holding with whoever came up (not tearing down the live ones)"
fi

if [ "$MODE" = "linux" ]; then
  step "JarvisLab up"
else
  step "cluster up"
fi
log "consumer  $INFEROUTE_CONSUMER_URL"
log "external  $INFEROUTE_PLATFORM_URL"
if [ "$MODE" = "linux" ]; then
  log "gpu       $JL_GPU (fallback: $JL_GPU_FALLBACK)"
  log "model     $INFEROUTE_MODEL_ALIAS  (vLLM)"
  log "hold: press Y then Enter to pause JarvisLab (Ctrl-C / Ctrl-D same)."
else
  log "model     $OLLAMA_MODEL_ALIAS  (linux/windows/mac all Ollama)"
  log "hold: press Y then Enter to pause Windows + Linux (Ctrl-C / Ctrl-D same). Mini stays up."
fi

if [ -t 0 ]; then
  while true; do
    if ! read -r -p "[cluster] Y to pause > " ans; then
      echo
      break
    fi
    case "$ans" in
      [Yy]|[Yy][Ee][Ss]) break ;;
      *)
        if [ "$MODE" = "linux" ]; then
          echo "[cluster] type Y (or Ctrl-C) to pause JarvisLab"
        else
          echo "[cluster] type Y (or Ctrl-C) to pause Windows + Linux"
        fi
        ;;
    esac
  done
else
  warn "stdin is not a TTY — sleeping until signal"
  while true; do sleep 3600; done
fi

exit 0
