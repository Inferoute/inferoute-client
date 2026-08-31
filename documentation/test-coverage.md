# Test coverage

Phase 1 coverage for `inferoute-client`: unit and httptest tests for the inference guard chain, node-facing HTTP clients, and shared helpers. No Docker, GPU hardware, Cloudflare tunnel, or end-to-end tests against a live platform yet.

## Running tests

Local (same checks as CI):

```bash
go vet ./...
go test -race -count=1 ./...
go build ./...
```

You can also ask Cursor to run the **`runtests`** skill for the same workflow.

## CI

GitHub Actions workflow: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)

- Triggers on push and pull request to `main`
- Runs `go vet`, `go test -race -count=1 ./...`, and `go build ./...`
- Uses `go-version-file: go.mod` (Go 1.22)
- **Informational only** — does not block merges; results appear under the repo **Actions** tab

## Test stack

- Go stdlib `testing` only (no testify or mockery)
- `net/http/httptest` for node and Ollama endpoint fakes
- `llm.Client` interface faked in server tests
- Zap nop logger via `TestMain` in packages that would otherwise write log files

---

## Covered

### `pkg/server`

| File | What is tested |
|------|----------------|
| `handler_test.go` | `handleChatCompletions` guard chain: missing HMAC → 401; invalid HMAC → 401; oversized `X-Request-Id` → 401 without calling the platform; valid HMAC → 200 and LLM response forwarded; HMAC before busy (missing/invalid HMAC while a slot is held → 401, not 503); `verifyModelInRequest` with nil verifier passes |
| `hmac_test.go` | `validateHMAC`: valid response; `valid=false`; non-200 status; malformed JSON |

### `pkg/pricing`

| File | What is tested |
|------|----------------|
| `client_test.go` | `GetModelPrices`; `RegisterModel` success; 400 + "already exists" → `ErrModelAlreadyExists`; other 4xx → `*ErrorResponse` |

### `pkg/llm`

| File | What is tested |
|------|----------------|
| `ollama_test.go` | `ForwardRequest` strips `gguf/` prefix; preserves non-gguf model names; non-200 → HTTP error |

### `pkg/verify`

| File | What is tested |
|------|----------------|
| `verifier_test.go` | Server response status mapping; result cache hit/miss/TTL; vLLM weight-change invalidation |
| `fingerprint_test.go` | Deterministic weight fingerprint; `NormalizeDigest` |
| `hfresolve_test.go` | Hugging Face cache dir resolution (pinned rev, `refs/main`, flat dir) |

### `pkg/geoloc`

| File | What is tested |
|------|----------------|
| `lookup_test.go` | `Lookup` via `httptest` and `INFEROUTE_GEO_LOOKUP_URL` override |

### `pkg/usermsg`

| File | What is tested |
|------|----------------|
| `format_test.go` | LLM unreachable / HTTP / unknown error → console and HTTP message strings; invalid API key startup message |

### `pkg/gpu`

| File | What is tested |
|------|----------------|
| `monitor_test.go` | `GetGPUInfo` 1s cache hit/expiry/error cache; returned copy is isolated from caller mutation; concurrent refresh is a single query; `IsBusy` uses the cache |

### `pkg/cloudflare`

| File | What is tested |
|------|----------------|
| `client_test.go` | Supervision loop vs startup timeout; `StartTunnel` rejects canceled context; `RequestTunnel` 401 / consumer-key → `ErrInvalidAPIKey`; success decodes token |
| `process_test.go` | cloudflared log path is OS-correct |

### `pkg/exechide`

| File | What is tested |
|------|----------------|
| `hide_*_test.go` | Windows: `CREATE_NO_WINDOW` + `HideWindow`; other OS: no-op |

---

## Not covered (high priority gaps)

| Area | Why it matters |
|------|----------------|
| `handleCompletions` | Same guard chain as chat completions; currently untested |
| GPU busy path (`503`) in handlers | Needs injectable or fake `*gpu.Monitor` |
| Unverified model path (`403`) in handlers | Needs fake `*verify.Verifier` or interface extraction |
| `pkg/health/reporter.go` — health report assembly, `registerNewModels` | Model registration and dedup logic |
| `pkg/pricing/registration.go` — `RegisterLocalModels` | Skips unverified models, default-price fallback |
| `pkg/verify/catalog.go`, `server.go`, `measure.go` | Catalog refresh and server-side verification |
| `pkg/llm/vllm.go` | vLLM client behavior |
| `pkg/gpu/monitor.go` | `nvidia-smi` XML parsing |
| `internal/config/config.go` | YAML load and defaults |
| `cmd/main.go` startup wiring | End-to-end process bootstrap |
| Integration against live `inferoute-node` | Wire protocol and auth |

---

## Test file index

| Package | Test files |
|---------|------------|
| `pkg/server` | `handler_test.go`, `hmac_test.go` |
| `pkg/pricing` | `client_test.go` |
| `pkg/llm` | `ollama_test.go` |
| `pkg/verify` | `verifier_test.go`, `fingerprint_test.go`, `hfresolve_test.go` |
| `pkg/geoloc` | `lookup_test.go` |
| `pkg/usermsg` | `format_test.go` |
| `pkg/gpu` | `monitor_test.go` |
| `pkg/cloudflare` | `client_test.go`, `process_test.go` |
| `pkg/exechide` | `hide_windows_test.go`, `hide_other_test.go` |

**Total:** 14 test files. `cmd/`, `internal/config`, and `pkg/health` still have no tests.
