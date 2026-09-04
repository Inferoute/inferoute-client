package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

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
	if got := vllm.CommandLine(); got != "vllm serve Qwen/Qwen2.5-7B-Instruct" {
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
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(srv.Close)

	if !Healthy(context.Background(), KindOllama, srv.URL) {
		t.Fatal("expected ollama healthy")
	}
	if Healthy(context.Background(), KindOllama, "http://127.0.0.1:1") {
		t.Fatal("refused port should be unhealthy")
	}

	vllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(vllm.Close)
	if !Healthy(context.Background(), KindFreeToken, vllm.URL) {
		t.Fatal("expected freetoken/vllm healthy")
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
