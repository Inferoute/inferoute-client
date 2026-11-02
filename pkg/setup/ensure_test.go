package setup

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
)

func dummyEngineBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "vllm")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestEnsureOnStartAlreadyHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3-0.6B"}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
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
	cfg.Provider.Engine = "vllm"
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
	cfg.Provider.Engine = "vllm"
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

func TestEnsureOnStartMasterJoinOpen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.EngineArgs = []string{
		"serve", "Qwen/Qwen2.5-Coder-32B-Instruct",
		"--nnodes", "2", "--node-rank", "0",
		"--master-addr", "127.0.0.1", "--master-port", strconv.Itoa(port),
	}

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ensureOnStart(ctx, cfg, Streams{In: strings.NewReader(""), Out: &out}, true); err != nil {
		t.Fatalf("ensureOnStart: %v", err)
	}
	if !strings.Contains(out.String(), "Waiting for workers to join") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestEnsureOnStartWaitsWhenPortOpen(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(empty.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
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

func stubClusterRefresh(t *testing.T) {
	t.Helper()
	old := refreshCluster
	refreshCluster = func(context.Context, *config.Config) error { return nil }
	t.Cleanup(func() { refreshCluster = old })
}

func TestEnsureOnStartWorkerPrintsCommand(t *testing.T) {
	stubClusterRefresh(t)
	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.EngineArgs = []string{"serve", "Qwen/Qwen3-0.6B", "--headless", "--master-addr", "198.18.0.2"}
	cfg.Provider.Model = "Qwen/Qwen3-0.6B"

	var out bytes.Buffer
	if err := ensureOnStart(context.Background(), cfg, Streams{In: strings.NewReader(""), Out: &out}, true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Start it now") {
		t.Fatalf("prompted to start:\n%s", text)
	}
	if !strings.Contains(text, "--headless") || !strings.Contains(text, docsClusterWorker) || !strings.Contains(text, "keeps running") {
		t.Fatalf("stdout = %q", text)
	}
}

func TestEnsureOnStartMasterNotStarted(t *testing.T) {
	stubClusterRefresh(t)
	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
	cfg.Provider.ClusterMode = "master"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.EngineArgs = []string{
		"serve", "Qwen/Qwen3-0.6B", "--nnodes", "2", "--node-rank", "0",
		"--master-addr", "127.0.0.1", "--master-port", "1",
	}
	cfg.Provider.Model = "Qwen/Qwen3-0.6B"

	var out bytes.Buffer
	if err := ensureOnStart(context.Background(), cfg, Streams{In: strings.NewReader(""), Out: &out}, true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Start it now") || !strings.Contains(text, docsClusterMaster) || !strings.Contains(text, "keeps running") {
		t.Fatalf("stdout = %q", text)
	}
}

func TestEnsureOnStartMasterReady(t *testing.T) {
	stubClusterRefresh(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3-0.6B"}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
	cfg.Provider.ClusterMode = "master"
	cfg.Provider.LLMURL = srv.URL

	var out bytes.Buffer
	if err := ensureOnStart(context.Background(), cfg, Streams{Out: &out}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is ready at") {
		t.Fatalf("stdout = %q", out.String())
	}
}
