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
	if len(linux) != 1 || linux[0].Kind != KindVLLM || linux[0].Unavailable != "" {
		t.Fatalf("linux+nvidia = %+v", linux)
	}
	linuxNoGPU := OptionsFor("linux", "amd64", false)
	if linuxNoGPU[0].Unavailable == "" {
		t.Fatal("linux without nvidia should mark vLLM unavailable")
	}

	mac := OptionsFor("darwin", "arm64", false)
	if len(mac) != 1 || mac[0].Kind != KindVLLMMetal || mac[0].Unavailable != "" {
		t.Fatalf("darwin arm64 = %+v", mac)
	}
	macIntel := OptionsFor("darwin", "amd64", false)
	if macIntel[0].Unavailable == "" {
		t.Fatal("intel mac should mark vLLM Metal unavailable")
	}

	win := OptionsFor("windows", "amd64", true)
	if len(win) != 1 || win[0].Kind != KindFreeToken || win[0].Unavailable != "" {
		t.Fatalf("windows = %+v", win)
	}
	winNoGPU := OptionsFor("windows", "amd64", false)
	if winNoGPU[0].Unavailable == "" {
		t.Fatal("windows without nvidia should mark FreeToken unavailable")
	}
}

func TestServeSpec(t *testing.T) {
	t.Parallel()

	vllm := ServeSpec(KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct", ServeOpts{})
	if got := vllm.CommandLine(); got != "vllm serve Qwen/Qwen2.5-7B-Instruct --host 127.0.0.1 --port 8000" {
		t.Errorf("vllm = %q", got)
	}

	ft := ServeSpec(KindFreeToken, "ft", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct", ServeOpts{})
	wantArgs := []string{"serve", "--model", "Qwen/Qwen2.5-7B-Instruct", "--served-model-name", "Qwen/Qwen2.5-7B-Instruct", "--host", "127.0.0.1", "--port", "1919", "--moe-backend", "fused"}
	if len(ft.Args) != len(wantArgs) {
		t.Fatalf("freetoken args = %v", ft.Args)
	}
	for i := range wantArgs {
		if ft.Args[i] != wantArgs[i] {
			t.Fatalf("freetoken args = %v", ft.Args)
		}
	}

	metal := ServeSpec(KindVLLMMetal, "/Users/me/.venv-vllm-metal/bin/vllm", "Qwen/Qwen3-0.6B", "Qwen/Qwen3-0.6B", ServeOpts{})
	if metal.Args[0] != "serve" || metal.Args[1] != "Qwen/Qwen3-0.6B" {
		t.Fatalf("metal args = %v", metal.Args)
	}
}

func TestServeSpecWithOpts(t *testing.T) {
	t.Parallel()

	yarn := ServeSpec(KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct", ServeOpts{
		ToolCallParser:     "hermes",
		MaxModelLen:        131072,
		RopeType:           "yarn",
		RopeBaseContextLen: 32768,
	})
	join := strings.Join(yarn.Args, " ")
	for _, s := range []string{
		"--max-model-len 131072",
		"--enable-auto-tool-choice",
		"--tool-call-parser hermes",
		`"rope_type":"yarn"`,
		`"factor":4`,
		`"original_max_position_embeddings":32768`,
	} {
		if !strings.Contains(join, s) {
			t.Fatalf("missing %q in args %v", s, yarn.Args)
		}
	}

	gemma := ServeSpec(KindVLLM, "vllm", "google/gemma-3-4b-it", "google/gemma-3-4b-it", ServeOpts{
		MaxModelLen: 131072,
	})
	join = strings.Join(gemma.Args, " ")
	if strings.Contains(join, "--hf-overrides") {
		t.Fatalf("gemma should not set hf-overrides: %v", gemma.Args)
	}
	if !strings.Contains(join, "--max-model-len 131072") {
		t.Fatalf("gemma missing max-model-len: %v", gemma.Args)
	}

	ft := ServeSpec(KindFreeToken, "ft", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct", ServeOpts{
		ToolCallParser:     "hermes",
		MaxModelLen:        131072,
		RopeType:           "yarn",
		RopeBaseContextLen: 32768,
	})
	join = strings.Join(ft.Args, " ")
	if strings.Contains(join, "--tool-call-parser") || strings.Contains(join, "--hf-overrides") {
		t.Fatalf("freetoken should ignore parser/rope: %v", ft.Args)
	}
	if !strings.Contains(join, "--max-seq-len-override 131072") {
		t.Fatalf("freetoken missing max-seq-len-override: %v", ft.Args)
	}
	if !strings.Contains(join, "--moe-backend fused") {
		t.Fatalf("freetoken missing moe-backend fused: %v", ft.Args)
	}

	offload := ServeSpec(KindFreeToken, "ft", "openai/gpt-oss-20b", "openai/gpt-oss-20b", ServeOpts{MoeStrategy: MoeAuto})
	join = strings.Join(offload.Args, " ")
	if !strings.Contains(join, "--moe-backend auto") || strings.Contains(join, "fused") {
		t.Fatalf("freetoken MoeAuto should emit --moe-backend auto: %v", offload.Args)
	}
	garbage := ServeSpec(KindFreeToken, "ft", "m", "m", ServeOpts{MoeStrategy: "cpu"})
	if !strings.Contains(strings.Join(garbage.Args, " "), "--moe-backend fused") {
		t.Fatalf("unknown strategy must fall back to fused: %v", garbage.Args)
	}
	vllmMoe := ServeSpec(KindVLLM, "vllm", "m", "m", ServeOpts{MoeStrategy: MoeAuto})
	if strings.Contains(strings.Join(vllmMoe.Args, " "), "moe") {
		t.Fatalf("vllm must ignore MoeStrategy: %v", vllmMoe.Args)
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
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3-0.6B"}]}`))
	}))
	t.Cleanup(srv.Close)

	if !Healthy(context.Background(), KindVLLM, srv.URL) {
		t.Fatal("expected vllm healthy")
	}
	if Healthy(context.Background(), KindVLLM, "http://127.0.0.1:1") {
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
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(empty.Close)
	if Healthy(context.Background(), KindVLLM, empty.URL) {
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
	if !Healthy(context.Background(), KindVLLM, vllm.URL) {
		t.Fatal("expected vllm healthy")
	}
	if Healthy(context.Background(), KindFreeToken, vllm.URL) {
		t.Fatal("FreeToken must not treat /v1/models as ready")
	}

	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(notReady.Close)
	if Healthy(context.Background(), KindFreeToken, notReady.URL) {
		t.Fatal("4xx must not count as healthy")
	}
}

func TestHealthyFreeTokenHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "ok", body: `{"status":"ok","model":"Qwen/Qwen2.5-Coder-7B-Instruct","uptime_s":12}`, want: true},
		{name: "loading", body: `{"status":"loading","phase":"weights","progress":{"done_bytes":1,"total_bytes":4},"model":"Qwen/Qwen2.5-Coder-7B-Instruct"}`, want: false},
		{name: "error", body: `{"status":"error","message":"backend worker is gone"}`, want: false},
		{name: "empty", body: `{}`, want: false},
		{name: "not json", body: `ok`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			got := Healthy(context.Background(), KindFreeToken, srv.URL)
			if got != tt.want {
				t.Fatalf("Healthy(freetoken, %s) = %v, want %v", tt.name, got, tt.want)
			}
		})
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
	if got := Label(Kind("")); got != "vLLM" {
		t.Errorf("Label(default) = %q, want vLLM", got)
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
	cfg.Provider.Engine = "vllm"
	cfg.Provider.LLMURL = "http://127.0.0.1:1"
	cfg.Provider.AutoStart = false
	if err := EnsureReady(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
}

func TestStartAndWaitAlreadyHealthy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3-0.6B"}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.Engine = "vllm"
	cfg.Provider.LLMURL = srv.URL
	cfg.Provider.AutoStart = false
	if err := StartAndWait(context.Background(), cfg, t.TempDir()); err != nil {
		t.Fatalf("StartAndWait: %v", err)
	}
}

func TestPlatformTypeCatalog(t *testing.T) {
	t.Parallel()
	if PlatformType(KindFreeToken) != "vllm" || CatalogType(KindVLLMMetal) != "vllm" || PlatformType(KindVLLM) != "vllm" {
		t.Fatal("engines must use vllm catalog")
	}
	if CompatKind("windows", "vllm") != KindFreeToken {
		t.Fatal("windows vllm catalog is FreeToken")
	}
	if CompatKind("darwin", "vllm") != KindVLLMMetal {
		t.Fatal("darwin vllm catalog is vLLM Metal")
	}
	if CompatKind("linux", "vllm") != KindVLLM {
		t.Fatal("linux vllm catalog is vLLM")
	}
	if CompatKind("windows", "custom") != KindFreeToken {
		t.Fatal("unknown provider type on windows is FreeToken")
	}
	if CompatKind("linux", "") != KindVLLM {
		t.Fatal("empty provider type on linux is vLLM")
	}
	if RuntimeKind("windows", KindVLLM) != KindFreeToken {
		t.Fatal("windows --engine vllm installs FreeToken")
	}
	if RuntimeKind("darwin", KindVLLM) != KindVLLMMetal {
		t.Fatal("darwin --engine vllm installs vLLM Metal")
	}
	if RuntimeKind("linux", KindVLLM) != KindVLLM {
		t.Fatal("linux --engine vllm stays CUDA vLLM")
	}
	if RuntimeKind("linux", KindFreeToken) != KindFreeToken {
		t.Fatal("explicit freetoken is not remapped")
	}
	if RuntimeKind("windows", KindVLLMMetal) != KindVLLMMetal {
		t.Fatal("explicit vllm-metal is not remapped")
	}
	_ = runtime.GOOS
}
