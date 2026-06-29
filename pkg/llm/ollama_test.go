package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})
	os.Exit(m.Run())
}

func newOllama(baseURL string) *OllamaClient {
	return &OllamaClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func TestForwardRequestStripsGgufPrefix(t *testing.T) {
	var receivedModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]interface{}
		json.Unmarshal(body, &parsed)
		receivedModel, _ = parsed["model"].(string)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	resp, err := newOllama(ts.URL).ForwardRequest(
		context.Background(),
		"/v1/chat/completions",
		[]byte(`{"model":"gguf/llama3"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if receivedModel != "llama3" {
		t.Fatalf("forwarded model = %q, want llama3 (gguf/ stripped)", receivedModel)
	}
	if string(resp) != `{"ok":true}` {
		t.Fatalf("response = %q", string(resp))
	}
}

func TestForwardRequestPreservesNonGgufModel(t *testing.T) {
	var receivedModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]interface{}
		json.Unmarshal(body, &parsed)
		receivedModel, _ = parsed["model"].(string)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	if _, err := newOllama(ts.URL).ForwardRequest(context.Background(), "/v1/completions", []byte(`{"model":"llama3"}`)); err != nil {
		t.Fatal(err)
	}
	if receivedModel != "llama3" {
		t.Fatalf("forwarded model = %q, want llama3 unchanged", receivedModel)
	}
}

func TestForwardRequestNon200IsHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := newOllama(ts.URL).ForwardRequest(context.Background(), "/v1/completions", []byte(`{"model":"m"}`))
	if err == nil {
		t.Fatal("expected error on non-200 status")
	}
}
