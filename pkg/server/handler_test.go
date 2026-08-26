package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})
	os.Exit(m.Run())
}

// fakeLLM is a stub llm.Client that records forwarded requests.
type fakeLLM struct {
	forwardResp []byte
	forwardErr  error
	gotPath     string
	gotBody     []byte
}

func (f *fakeLLM) ListModels(ctx context.Context) (*llm.ListModelsResponse, error) { return nil, nil }
func (f *fakeLLM) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (f *fakeLLM) ForwardRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
	f.gotPath, f.gotBody = path, body
	return f.forwardResp, f.forwardErr
}

// nodeStub returns an httptest server that answers the HMAC validation endpoint.
func nodeStub(t *testing.T, valid bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/provider/validate_hmac" {
			json.NewEncoder(w).Encode(HMACValidationResponse{Valid: valid})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newTestServer(nodeURL string, llmClient llm.Client) *Server {
	cfg := &config.Config{}
	cfg.Provider.URL = nodeURL
	cfg.Provider.APIKey = "test-key"
	cfg.Provider.ProviderType = "ollama"
	return &Server{config: cfg, llmClient: llmClient, maxInflight: 1}
}

func postChat(s *Server, hmac, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	if hmac != "" {
		req.Header.Set("X-Request-Id", hmac)
	}
	rec := httptest.NewRecorder()
	s.handleChatCompletions(rec, req)
	return rec
}

func TestHandleChatCompletionsGuardChain(t *testing.T) {
	t.Run("missing HMAC returns 401", func(t *testing.T) {
		s := newTestServer("http://unused", &fakeLLM{})
		if got := postChat(s, "", `{"model":"m"}`).Code; got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})

	t.Run("invalid HMAC returns 401", func(t *testing.T) {
		node := nodeStub(t, false)
		s := newTestServer(node.URL, &fakeLLM{})
		if got := postChat(s, "bad-hmac", `{"model":"m"}`).Code; got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})

	t.Run("valid HMAC forwards to LLM and returns 200", func(t *testing.T) {
		node := nodeStub(t, true)
		fake := &fakeLLM{forwardResp: []byte(`{"ok":true}`)}
		s := newTestServer(node.URL, fake)

		rec := postChat(s, "good-hmac", `{"model":"m"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != `{"ok":true}` {
			t.Fatalf("body = %q, want forwarded LLM response", rec.Body.String())
		}
		if fake.gotPath != "/v1/chat/completions" {
			t.Fatalf("forwarded path = %q", fake.gotPath)
		}
	})
}

func TestVerifyModelInRequestNilVerifierPasses(t *testing.T) {
	s := newTestServer("http://unused", &fakeLLM{})
	if err := s.verifyModelInRequest(context.Background(), []byte(`{"model":"m"}`)); err != nil {
		t.Fatalf("nil verifier should pass, got %v", err)
	}
}

type blockingLLM struct {
	started chan struct{}
	release chan struct{}
	inner   fakeLLM
}

func (b *blockingLLM) ListModels(ctx context.Context) (*llm.ListModelsResponse, error) {
	return b.inner.ListModels(ctx)
}
func (b *blockingLLM) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	return b.inner.Chat(ctx, r)
}
func (b *blockingLLM) ForwardRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return b.inner.ForwardRequest(ctx, path, body)
}

func TestInflightCapRejectsConcurrentInference(t *testing.T) {
	node := nodeStub(t, true)
	block := &blockingLLM{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		inner:   fakeLLM{forwardResp: []byte(`{"ok":true}`)},
	}
	s := newTestServer(node.URL, block)

	done := make(chan int, 1)
	go func() {
		done <- postChat(s, "hmac", `{"model":"m"}`).Code
	}()

	select {
	case <-block.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached LLM")
	}

	busyRec := httptest.NewRecorder()
	s.handleBusy(busyRec, httptest.NewRequest(http.MethodGet, "/api/busy", nil))
	if busyRec.Code != http.StatusOK {
		t.Fatalf("busy status = %d", busyRec.Code)
	}
	var busy BusyResponse
	if err := json.NewDecoder(busyRec.Body).Decode(&busy); err != nil {
		t.Fatalf("decode busy: %v", err)
	}
	if !busy.Busy {
		t.Fatal("expected busy while inference in flight")
	}

	if got := postChat(s, "hmac", `{"model":"m"}`).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("second request status = %d, want 503", got)
	}

	close(block.release)
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("first request status = %d, want 200", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}

	if got := postChat(s, "hmac", `{"model":"m"}`).Code; got != http.StatusOK {
		t.Fatalf("after release status = %d, want 200", got)
	}
}

func TestTryAcquireInference(t *testing.T) {
	s := &Server{maxInflight: 1}
	if !s.tryAcquireInference() {
		t.Fatal("first acquire should succeed")
	}
	if s.tryAcquireInference() {
		t.Fatal("second acquire should fail")
	}
	s.releaseInference()
	if !s.tryAcquireInference() {
		t.Fatal("acquire after release should succeed")
	}

	unlimited := &Server{maxInflight: 0}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !unlimited.tryAcquireInference() {
				t.Error("unlimited acquire failed")
			}
		}()
	}
	wg.Wait()
}
