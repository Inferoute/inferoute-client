package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
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
	if len(mac) != 2 || mac[0].Kind != KindVLLMMetal || mac[0].Unavailable != "" || mac[1].Kind != KindLlamaCpp {
		t.Fatalf("darwin arm64 = %+v", mac)
	}
	macIntel := OptionsFor("darwin", "amd64", false)
	if macIntel[0].Unavailable == "" || macIntel[1].Unavailable == "" {
		t.Fatal("intel mac should mark both engines unavailable")
	}

	win := OptionsFor("windows", "amd64", true)
	if len(win) != 1 || win[0].Kind != KindLlamaCpp || win[0].Unavailable != "" {
		t.Fatalf("windows = %+v", win)
	}
	winNoGPU := OptionsFor("windows", "amd64", false)
	if winNoGPU[0].Unavailable == "" {
		t.Fatal("windows without nvidia should mark llama.cpp unavailable")
	}
}

func TestServeSpec(t *testing.T) {
	t.Parallel()

	vllm := ServeSpec(KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct", ServeOpts{})
	if got := vllm.CommandLine(); got != "vllm serve Qwen/Qwen2.5-7B-Instruct --host 127.0.0.1 --port 8000" {
		t.Errorf("vllm = %q", got)
	}

	ft := ServeSpec(KindLlamaCpp, "llama-server", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct-GGUF", ServeOpts{GGUFFile: "qwen2.5-7b-instruct-q4_k_m.gguf"})
	wantArgs := []string{"--hf-repo", "Qwen/Qwen2.5-7B-Instruct-GGUF", "--hf-file", "qwen2.5-7b-instruct-q4_k_m.gguf", "--alias", "Qwen/Qwen2.5-7B-Instruct", "--host", "127.0.0.1", "--port", "8081", "-ngl", "99"}
	if len(ft.Args) != len(wantArgs) {
		t.Fatalf("llama.cpp args = %v", ft.Args)
	}
	for i := range wantArgs {
		if ft.Args[i] != wantArgs[i] {
			t.Fatalf("llama.cpp args = %v", ft.Args)
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
	if !strings.Contains(strings.Join(yarn.Env, " "), "VLLM_ALLOW_LONG_MAX_MODEL_LEN=1") {
		t.Fatalf("yarn context missing long-len override: %v", yarn.Env)
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

	llama := ServeSpec(KindLlamaCpp, "llama-server", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct-GGUF", ServeOpts{
		ToolCallParser: "hermes",
		MaxModelLen:    131072,
		GGUFFile:       "qwen2.5-7b-instruct-q4_k_m.gguf",
	})
	join = strings.Join(llama.Args, " ")
	if strings.Contains(join, "--tool-call-parser") || strings.Contains(join, "--hf-overrides") {
		t.Fatalf("llama.cpp should not pass vLLM parser flags: %v", llama.Args)
	}
	if !strings.Contains(join, "--jinja") || !strings.Contains(join, "-c 131072") {
		t.Fatalf("llama.cpp missing jinja or context: %v", llama.Args)
	}
	plain := ServeSpec(KindLlamaCpp, "llama-server", "m", "org/m", ServeOpts{GGUFFile: "m.gguf"})
	if strings.Contains(strings.Join(plain.Args, " "), "--jinja") {
		t.Fatalf("llama.cpp without a parser must not pass --jinja: %v", plain.Args)
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
	if Healthy(context.Background(), KindLlamaCpp, vllm.URL) {
		t.Fatal("llama.cpp must not treat /v1/models as ready")
	}

	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(notReady.Close)
	if Healthy(context.Background(), KindLlamaCpp, notReady.URL) {
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
			got := Healthy(context.Background(), KindLlamaCpp, srv.URL)
			if got != tt.want {
				t.Fatalf("Healthy(llama_cpp, %s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMasterJoinAddr(t *testing.T) {
	t.Parallel()
	master := []string{
		"serve", "Qwen/Qwen2.5-Coder-32B-Instruct",
		"--nnodes", "2", "--node-rank", "0",
		"--master-addr", "198.18.0.1", "--master-port", "29501",
	}
	if got := MasterJoinAddr(master); got != "198.18.0.1:29501" {
		t.Fatalf("MasterJoinAddr = %q", got)
	}
	if got := MasterJoinAddr(append(master, "--headless")); got != "" {
		t.Fatalf("headless MasterJoinAddr = %q", got)
	}
	worker := append([]string{}, master...)
	for i := 0; i < len(worker)-1; i++ {
		if worker[i] == "--node-rank" {
			worker[i+1] = "1"
		}
	}
	if got := MasterJoinAddr(worker); got != "" {
		t.Fatalf("worker MasterJoinAddr = %q", got)
	}
}

func TestWorkerSlots(t *testing.T) {
	t.Parallel()
	joined, need := WorkerSlots(0, 2)
	if joined != 0 || need != 1 {
		t.Fatalf("WorkerSlots(0, 2) = %d/%d", joined, need)
	}
	joined, need = WorkerSlots(3, 2)
	if joined != 1 || need != 1 {
		t.Fatalf("WorkerSlots(3, 2) = %d/%d", joined, need)
	}
	if _, need = WorkerSlots(1, 1); need != 0 {
		t.Fatalf("single node need = %d", need)
	}
}

func TestWaitListen(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitListen(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)), 20*time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWaitHealthyProcessExit(t *testing.T) {
	t.Parallel()
	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit 1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := WaitHealthy(ctx, KindLlamaCpp, "http://127.0.0.1:1", 50*time.Millisecond, exited)
	if err == nil || !strings.Contains(err.Error(), "process exited") {
		t.Fatalf("got %v", err)
	}
}

func TestLabel(t *testing.T) {
	t.Parallel()
	if got := Label(Kind("")); got != "vLLM" {
		t.Errorf("Label(default) = %q, want vLLM", got)
	}
	if got := Label(KindLlamaCpp); got != "llama.cpp" {
		t.Errorf("Label(llama_cpp) = %q, want llama.cpp", got)
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
	if PlatformType(KindLlamaCpp) != "llama_cpp" || CatalogType(KindVLLMMetal) != "vllm" || PlatformType(KindVLLM) != "vllm" {
		t.Fatal("llama.cpp uses llama_cpp; vLLM engines use vllm")
	}
	if CompatKind("windows", "vllm") != KindLlamaCpp {
		t.Fatal("windows vllm catalog is llama.cpp")
	}
	if CompatKind("darwin", "vllm") != KindVLLMMetal {
		t.Fatal("darwin vllm catalog is vLLM Metal")
	}
	if CompatKind("linux", "vllm") != KindVLLM {
		t.Fatal("linux vllm catalog is vLLM")
	}
	if CompatKind("windows", "custom") != KindLlamaCpp {
		t.Fatal("unknown provider type on windows is llama.cpp")
	}
	if CompatKind("linux", "") != KindVLLM {
		t.Fatal("empty provider type on linux is vLLM")
	}
	if RuntimeKind("windows", KindVLLM) != KindLlamaCpp {
		t.Fatal("windows --engine vllm installs llama.cpp")
	}
	if RuntimeKind("darwin", KindVLLM) != KindVLLMMetal {
		t.Fatal("darwin --engine vllm installs vLLM Metal")
	}
	if RuntimeKind("linux", KindVLLM) != KindVLLM {
		t.Fatal("linux --engine vllm stays CUDA vLLM")
	}
	if RuntimeKind("linux", KindLlamaCpp) != KindLlamaCpp {
		t.Fatal("explicit llama_cpp is not remapped")
	}
	if RuntimeKind("windows", KindVLLMMetal) != KindVLLMMetal {
		t.Fatal("explicit vllm-metal is not remapped")
	}
	_ = runtime.GOOS
}
