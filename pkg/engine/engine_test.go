package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

func TestOptionsFor(t *testing.T) {
	t.Parallel()

	linux := OptionsFor("linux", "amd64", true)
	if len(linux) != 2 || linux[0].Kind != KindOllama || linux[1].Kind != KindVLLM || linux[1].Unavailable != "" {
		t.Fatalf("linux+nvidia = %+v", linux)
	}
	linuxNoGPU := OptionsFor("linux", "amd64", false)
	if linuxNoGPU[1].Unavailable == "" {
		t.Fatal("linux without nvidia should mark vLLM unavailable")
	}

	mac := OptionsFor("darwin", "arm64", false)
	if len(mac) != 2 || mac[1].Kind != KindVLLMMetal || mac[1].Unavailable != "" {
		t.Fatalf("darwin arm64 = %+v", mac)
	}
	macIntel := OptionsFor("darwin", "amd64", false)
	if macIntel[1].Unavailable == "" {
		t.Fatal("intel mac should mark vLLM Metal unavailable")
	}

	win := OptionsFor("windows", "amd64", true)
	if len(win) != 2 || win[1].Kind != KindFreeToken {
		t.Fatalf("windows = %+v", win)
	}
}

func TestServeSpec(t *testing.T) {
	t.Parallel()

	ollama := ServeSpec(KindOllama, "ollama", "gguf/qwen3:0.6b", "")
	if got := ollama.CommandLine(); got != "ollama serve" {
		t.Errorf("ollama serve = %q", got)
	}
	pull := PullSpec("ollama", "gguf/qwen3:0.6b")
	if got := pull.CommandLine(); got != "ollama pull qwen3:0.6b" {
		t.Errorf("ollama pull = %q", got)
	}

	vllm := ServeSpec(KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct")
	if got := vllm.CommandLine(); got != "vllm serve Qwen/Qwen2.5-7B-Instruct --host 127.0.0.1 --port 8000" {
		t.Errorf("vllm = %q", got)
	}

	ft := ServeSpec(KindFreeToken, "ft", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct")
	wantArgs := []string{"serve", "--model", "Qwen/Qwen2.5-7B-Instruct", "--served-model-name", "Qwen/Qwen2.5-7B-Instruct", "--host", "127.0.0.1", "--port", "1919"}
	if len(ft.Args) != len(wantArgs) {
		t.Fatalf("freetoken args = %v", ft.Args)
	}
	for i := range wantArgs {
		if ft.Args[i] != wantArgs[i] {
			t.Fatalf("freetoken args = %v", ft.Args)
		}
	}

	metal := ServeSpec(KindVLLMMetal, "/Users/me/.venv-vllm-metal/bin/vllm", "Qwen/Qwen3-0.6B", "Qwen/Qwen3-0.6B")
	if metal.Args[0] != "serve" || metal.Args[1] != "Qwen/Qwen3-0.6B" {
		t.Fatalf("metal args = %v", metal.Args)
	}
}

func TestOllamaPullName(t *testing.T) {
	t.Parallel()
	if got := OllamaPullName("gguf/qwen3:0.6b"); got != "qwen3:0.6b" {
		t.Errorf("got %q", got)
	}
	if got := OllamaPullName("qwen3:0.6b"); got != "qwen3:0.6b" {
		t.Errorf("got %q", got)
	}
}

func TestHFRepo(t *testing.T) {
	t.Parallel()
	repo := "Qwen/Qwen3-0.6B"
	entry := verify.CatalogEntry{Alias: "Qwen/Qwen3-0.6B", HFRepo: &repo}
	if got := HFRepo(entry); got != repo {
		t.Errorf("got %q", got)
	}
}

func TestHealthy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:0.6b"}]}`))
	}))
	t.Cleanup(srv.Close)

	if !Healthy(context.Background(), KindOllama, srv.URL) {
		t.Fatal("expected ollama healthy")
	}
	if Healthy(context.Background(), KindOllama, "http://127.0.0.1:1") {
		t.Fatal("refused port should be unhealthy")
	}
	if PortOpen(context.Background(), srv.URL) {
		// httptest listens; PortOpen should see it
	} else {
		t.Fatal("expected PortOpen on httptest server")
	}
	if PortOpen(context.Background(), "http://127.0.0.1:1") {
		t.Fatal("refused port should not be open")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[],"data":[]}`))
	}))
	t.Cleanup(empty.Close)
	if Healthy(context.Background(), KindOllama, empty.URL) {
		t.Fatal("empty model list must not count as healthy")
	}
	if Healthy(context.Background(), KindFreeToken, empty.URL) {
		t.Fatal("empty model list must not count as healthy")
	}

	vllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3.6-35B-A3B"}]}`))
	}))
	t.Cleanup(vllm.Close)
	if !Healthy(context.Background(), KindFreeToken, vllm.URL) {
		t.Fatal("expected freetoken/vllm healthy")
	}

	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(notReady.Close)
	if Healthy(context.Background(), KindFreeToken, notReady.URL) {
		t.Fatal("4xx must not count as healthy")
	}
}

func TestWaitHealthyProcessExit(t *testing.T) {
	t.Parallel()
	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit 1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WaitHealthy(ctx, KindFreeToken, "http://127.0.0.1:1", 50*time.Millisecond, exited)
	if err == nil || !strings.Contains(err.Error(), "process exited") {
		t.Fatalf("got %v", err)
	}
}

func TestLabel(t *testing.T) {
	t.Parallel()
	if got := Label(KindOllama); got != "Ollama" {
		t.Errorf("Label(ollama) = %q, want Ollama", got)
	}
	if got := Label(KindFreeToken); got != "FreeToken" {
		t.Errorf("Label(freetoken) = %q, want FreeToken", got)
	}
	if got := Label(KindVLLMMetal); got != "vLLM Metal" {
		t.Errorf("Label(vllm-metal) = %q, want vLLM Metal", got)
	}
}

func TestEnsureReadySkipsWithoutAutoStart(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.AutoStart = false
	if err := EnsureReady(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
}

func TestStartAndWaitAlreadyHealthy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:0.6b"}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "ollama"
	cfg.Provider.LLMURL = srv.URL
	cfg.Provider.AutoStart = false
	if err := StartAndWait(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("StartAndWait: %v", err)
	}
}

func TestPlatformTypeCatalog(t *testing.T) {
	t.Parallel()
	if PlatformType(KindFreeToken) != "vllm" || CatalogType(KindVLLMMetal) != "vllm" {
		t.Fatal("openai-compatible engines must use vllm catalog")
	}
	if PlatformType(KindOllama) != "ollama" {
		t.Fatal("ollama")
	}
	_ = runtime.GOOS
}
