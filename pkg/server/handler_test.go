package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
	return &Server{config: cfg, llmClient: llmClient}
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
