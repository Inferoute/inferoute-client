package setup

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
)

func dummyEngineBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ollama")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestEnsureOnStartAlreadyHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:0.6b"}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = srv.URL

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ensureOnStart(ctx, cfg, Streams{In: strings.NewReader(""), Out: &out}, true); err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
	if !strings.Contains(out.String(), "is running at") {
		t.Errorf("stdout = %q, want running message", out.String())
	}
}

func TestEnsureOnStartDecline(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.EngineBin = dummyEngineBin(t)
	cfg.Provider.AutoStart = true

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := ensureOnStart(ctx, cfg, Streams{In: strings.NewReader("n\n"), Out: &out}, true)
	if err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
	if !strings.Contains(out.String(), "Skipping") {
		t.Errorf("stdout = %q, want skip message", out.String())
	}
}

func TestEnsureOnStartNonInteractiveNoAutoStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.EngineBin = dummyEngineBin(t)
	cfg.Provider.AutoStart = false

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ensureOnStart(ctx, cfg, Streams{In: strings.NewReader(""), Out: &out}, false); err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
	if !strings.Contains(out.String(), "Skipping") {
		t.Errorf("stdout = %q, want skip message", out.String())
	}
}

func TestEnsureOnStartUnknownEngine(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Engine = "nope"
	var out bytes.Buffer
	if err := ensureOnStart(context.Background(), cfg, Streams{Out: &out}, true); err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestEnsureOnStartNilConfig(t *testing.T) {
	if err := ensureOnStart(context.Background(), nil, Streams{}, false); err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
}

func TestEnsureOnStartWaitsWhenPortOpen(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[],"data":[]}`))
	}))
	t.Cleanup(empty.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = empty.URL

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := ensureOnStart(ctx, cfg, Streams{In: strings.NewReader(""), Out: &out}, false)
	if err == nil {
		t.Fatal("ensureOnStart: want timeout while port is open but no models")
	}
	if !strings.Contains(out.String(), "already starting") {
		t.Errorf("stdout = %q, want already starting", out.String())
	}
}
