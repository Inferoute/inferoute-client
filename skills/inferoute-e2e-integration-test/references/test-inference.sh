#!/usr/bin/env bash
# Mac-side inference smoke tests. Usage: source e2e.env && ./test-inference.sh
set -euo pipefail

: "${INFEROUTE_CONSUMER_URL:?}"
: "${CONSUMER_API_KEY:?}"
: "${INFEROUTE_MODEL_ALIAS:?}"

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

echo "=== GET /v1/models ==="
if curl -sf "${AUTH[@]}" "$BASE/v1/models" | jq -e '.data | length > 0' >/dev/null; then
  check "models list" 0
else
  check "models list" 1
fi

echo "=== POST /v1/chat/completions (sync) ==="
resp=$(curl -s -w "\n%{http_code}" "${AUTH[@]}" "$BASE/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"Reply with exactly: inferoute-ok"}],"stream":false,"max_tokens":32}')
body=$(echo "$resp" | sed '$d')
code=$(echo "$resp" | tail -1)
if [ "$code" = "200" ] && echo "$body" | jq -e '.choices[0].message.content' >/dev/null; then
  check "chat sync" 0
  echo "$body" | jq -r '.choices[0].message.content'
else
  check "chat sync" 1
  echo "$body"
fi

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

echo "=== POST /v1/chat/completions (stream) ==="
stream_out=$(curl -N -s -w "\nHTTP_CODE:%{http_code}" "${AUTH[@]}" "$BASE/v1/chat/completions" \
  -d '{"model":"'"$INFEROUTE_MODEL_ALIAS"'","messages":[{"role":"user","content":"Say hello"}],"stream":true,"max_tokens":32}' | tee /dev/stderr)
if echo "$stream_out" | grep -q 'data: \[DONE\]' || echo "$stream_out" | grep -q 'data:{'; then
  check "chat stream" 0
else
  check "chat stream" 1
fi

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

echo ""
echo "Results: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
