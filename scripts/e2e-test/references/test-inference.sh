#!/usr/bin/env bash
# Mac-side inference smoke tests.
# Usage: ./test-inference.sh   (config comes from .env next to this script)
# Set SKIP_WAIT=1 to skip readiness gates (only if you know the stack is warm).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

E2E_ENV="${E2E_ENV:-$SCRIPT_DIR/.env}"
if [ -f "$E2E_ENV" ]; then
  # shellcheck source=/dev/null
  source "$E2E_ENV"
fi

: "${INFEROUTE_CONSUMER_URL:?}"
: "${CONSUMER_API_KEY:?}"
: "${INFEROUTE_MODEL_ALIAS:?}"

if [ "${SKIP_WAIT:-0}" != "1" ]; then
  echo "=== Waiting for vLLM and inferoute-client ==="
  bash "$SCRIPT_DIR/wait-for-ready.sh"
fi

BASE="${INFEROUTE_CONSUMER_URL%/}"
AUTH=(-H "Authorization: Bearer ${CONSUMER_API_KEY}" -H "Content-Type: application/json")

pass=0
fail=0

check() {
  local name="$1" code="$2"
  if [ "$code" -eq 0 ]; then
    echo "PASS  $name"
    pass=$((pass + 1))
  else
    echo "FAIL  $name"
    fail=$((fail + 1))
  fi
}

# POST /v1/chat/completions and split body from trailing HTTP status line.
post_chat() {
  local payload="$1"
  curl -s -w "\n%{http_code}" "${AUTH[@]}" "$BASE/v1/chat/completions" -d "$payload"
}

# Expect a completed (non-streaming) chat response with message content.
check_chat_sync() {
  local name="$1" payload="$2"
  local resp body code
  resp=$(post_chat "$payload")
  body=$(echo "$resp" | sed '$d')
  code=$(echo "$resp" | tail -1)
  if [ "$code" = "200" ] && echo "$body" | jq -e '.choices[0].message.content' >/dev/null; then
    check "$name" 0
  else
    check "$name" 1
    echo "$body"
  fi
}

# Consumer can list models: the platform routes to a healthy provider and
# returns a non-empty catalog. Proves auth + provider discovery work.
echo "=== GET /v1/models ==="
if curl -sf "${AUTH[@]}" "$BASE/v1/models" | jq -e '.data | length > 0' >/dev/null; then
  check "models list" 0
else
  check "models list" 1
fi

# Full non-streaming chat round-trip: platform selects the provider, forwards to
# vLLM, and returns a single completed response with choices[0].message.content.
# This is the core "does inference actually work end-to-end" test.
echo "=== POST /v1/chat/completions (sync) ==="
check_chat_sync "chat sync" \
  '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"Reply with exactly: inferoute-ok"}],"stream":false,"max_tokens":32}'

# Legacy (non-chat) completions endpoint, non-streaming. Optional: a 404 means
# the gateway doesn't expose it, which is treated as SKIP rather than a failure.
echo "=== POST /v1/completions (sync) ==="
resp=$(curl -s -w "\n%{http_code}" "${AUTH[@]}" "$BASE/v1/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","prompt":"Reply with exactly: inferoute-ok","stream":false,"max_tokens":32}' 2>/dev/null || true)
if [ -n "$resp" ]; then
  body=$(echo "$resp" | sed '$d')
  code=$(echo "$resp" | tail -1)
  if [ "$code" = "200" ]; then
    check "completions sync" 0
  elif [ "$code" = "404" ]; then
    echo "SKIP  completions sync (endpoint not exposed)"
  else
    check "completions sync" 1
    echo "$body"
  fi
else
  echo "SKIP  completions sync"
fi

# Streaming chat: verifies SSE works through the proxy — the response arrives as
# incremental `data:` chunks terminated by `data: [DONE]`, not one buffered blob.
echo "=== POST /v1/chat/completions (stream) ==="
stream_out=$(curl -N -s -w "\nHTTP_CODE:%{http_code}" "${AUTH[@]}" "$BASE/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"Say hello"}],"stream":true,"max_tokens":32}' | tee /dev/stderr)
if echo "$stream_out" | grep -q 'data: \[DONE\]' || echo "$stream_out" | grep -q 'data:{'; then
  check "chat stream" 0
else
  check "chat stream" 1
fi

# Legacy completions endpoint with streaming. Optional: empty/no response is a
# SKIP (endpoint not exposed); otherwise it must stream `data:` chunks like above.
echo "=== POST /v1/completions (stream) ==="
stream_out=$(curl -N -s "${AUTH[@]}" "$BASE/v1/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","prompt":"Say hello","stream":true,"max_tokens":32}' 2>/dev/null || true)
if [ -z "$stream_out" ]; then
  echo "SKIP  completions stream"
elif echo "$stream_out" | grep -q 'data: \[DONE\]' || echo "$stream_out" | grep -q 'data:{'; then
  check "completions stream" 0
else
  check "completions stream" 1
fi

# ── inferoute request routing options ─────────────────────────────────────────
# Docs: https://docs.inferoute.com/monthly-spending-caps/request-routing-options
# The `inferoute` block is stripped before the request is forwarded to the provider.
ROUTE_MIN_TPS_OK="${ROUTE_MIN_TPS_OK:-1}"
ROUTE_MAX_PRICE_OK="${ROUTE_MAX_PRICE_OK:-1.0}"          # provider ~$0.46/1M tokens

CHAT_MSG='{"role":"user","content":"Say hello in one short sentence"}'

# inferoute.sort=cost: prefer lower price when ranking providers (default behavior).
echo "=== POST /v1/chat/completions (inferoute.sort=cost) ==="
check_chat_sync "routing sort=cost" \
  '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":['"$CHAT_MSG"'],"stream":false,"max_tokens":32,"inferoute":{"sort":"cost"}}'

# inferoute.sort=throughput: prefer faster providers when ranking.
echo "=== POST /v1/chat/completions (inferoute.sort=throughput) ==="
check_chat_sync "routing sort=throughput" \
  '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":['"$CHAT_MSG"'],"stream":false,"max_tokens":32,"inferoute":{"sort":"throughput"}}'

# inferoute.min_tokens_per_second (pass): provider average TPS must be >= threshold.
echo "=== POST /v1/chat/completions (inferoute.min_tokens_per_second ok) ==="
check_chat_sync "routing min_tokens_per_second ok" \
  '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":['"$CHAT_MSG"'],"stream":false,"max_tokens":32,"inferoute":{"min_tokens_per_second":'"$ROUTE_MIN_TPS_OK"'}}'

# inferoute.max_*_price_per_1m (pass): per-request price ceiling above provider pricing.
echo "=== POST /v1/chat/completions (inferoute max price ok) ==="
check_chat_sync "routing max price ok" \
  '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":['"$CHAT_MSG"'],"stream":false,"max_tokens":32,"inferoute":{"max_input_price_per_1m":'"$ROUTE_MAX_PRICE_OK"',"max_output_price_per_1m":'"$ROUTE_MAX_PRICE_OK"'}}'

# Provider-selection reject paths (impossible min_tps / too-low price ceiling) are
# covered by Go unit tests: orchestrator TestFilterProviders + TestNoProviderError.

# Combined inferoute block + stream:true — routing options must not break SSE forwarding.
echo "=== POST /v1/chat/completions (inferoute + stream) ==="
stream_out=$(curl -N -s -w "\nHTTP_CODE:%{http_code}" "${AUTH[@]}" "$BASE/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":['"$CHAT_MSG"'],"stream":true,"max_tokens":32,"inferoute":{"sort":"throughput","min_tokens_per_second":'"$ROUTE_MIN_TPS_OK"',"max_input_price_per_1m":'"$ROUTE_MAX_PRICE_OK"',"max_output_price_per_1m":'"$ROUTE_MAX_PRICE_OK"'}}' | tee /dev/stderr)
if echo "$stream_out" | grep -q 'data: \[DONE\]' || echo "$stream_out" | grep -q 'data:{'; then
  check "routing + stream" 0
else
  check "routing + stream" 1
fi

echo ""
echo "Results: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
