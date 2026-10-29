package setup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
)

func TestMain(m *testing.M) {
	prepareCluster = func(*config.Config, *engine.Spec) error { return nil }
	clusterPlan = func(*config.Config) (clusterSettings, error) {
		return clusterSettings{Role: "single"}, nil
	}
	os.Exit(m.Run())
}

func TestExecuteMultiPreparesCluster(t *testing.T) {
	called := false
	prepareCluster = func(cfg *config.Config, spec *engine.Spec) error {
		called = true
		cfg.Provider.ClusterMode = "master"
		return nil
	}
	t.Cleanup(func() {
		prepareCluster = func(*config.Config, *engine.Spec) error { return nil }
	})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out, errBuf bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "sk-test",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf})
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, errBuf.String())
	}
	if !called {
		t.Fatal("prepareCluster was not called")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.ClusterMode != "master" {
		t.Fatalf("cluster_mode = %q", cfg.Provider.ClusterMode)
	}
}

func singlePlan(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		clusterPlan = func(*config.Config) (clusterSettings, error) {
			return clusterSettings{Role: "single"}, nil
		}
		detectEngine = engine.Detect
		installEngine = engine.Install
	})
}

func TestExecuteMasterUsesClusterModel(t *testing.T) {
	singlePlan(t)
	clusterPlan = func(*config.Config) (clusterSettings, error) {
		return clusterSettings{
			Role: "master", Name: "inferoute-cluster1",
			Model: "Qwen/Qwen3-0.6B", DisplayName: "Qwen3 0.6B", ServiceType: "vllm",
		}, nil
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out, errBuf bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		APIKey:         "sk-cluster",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf})
	if err != nil {
		t.Fatalf("Execute: %v\n%s\n%s", err, out.String(), errBuf.String())
	}
	if strings.Contains(out.String(), "Models that should fit") {
		t.Fatalf("cluster setup showed the single-machine list:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Cluster inferoute-cluster1") || !strings.Contains(out.String(), "Qwen3 0.6B (Qwen/Qwen3-0.6B)") {
		t.Fatalf("stdout = %s", out.String())
	}
	if !strings.Contains(out.String(), "vllm serve Qwen/Qwen3-0.6B") && !strings.Contains(out.String(), "serve Qwen/Qwen3-0.6B") {
		t.Fatalf("missing serve command:\n%s", out.String())
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "Qwen/Qwen3-0.6B" {
		t.Fatalf("model = %q", cfg.Provider.Model)
	}
}

func TestExecuteWorkerInstallsAndConfirms(t *testing.T) {
	singlePlan(t)
	clusterPlan = func(*config.Config) (clusterSettings, error) {
		return clusterSettings{
			Role: "worker", Name: "inferoute-cluster1",
			Model: "Qwen/Qwen3-0.6B", DisplayName: "Qwen3 0.6B", ServiceType: "vllm",
		}, nil
	}
	calls := 0
	detectEngine = func(kind engine.Kind) engine.Detected {
		calls++
		if calls == 1 {
			return engine.Detected{Kind: kind}
		}
		return engine.Detected{Kind: kind, Found: true, Bin: "vllm"}
	}
	installed := false
	installEngine = func(_ context.Context, kind engine.Kind, _ io.Writer) error {
		installed = true
		if kind != engine.RuntimeKind(runtime.GOOS, engine.KindVLLM) {
			t.Errorf("installed %s", kind)
		}
		return nil
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out, errBuf bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		APIKey:         "join-abc",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
	}, Streams{In: strings.NewReader("n\n"), Out: &out, Err: &errBuf})
	if err != nil {
		t.Fatalf("Execute: %v\n%s\n%s", err, out.String(), errBuf.String())
	}
	if !installed {
		t.Fatal("vLLM was not installed")
	}
	text := out.String()
	if strings.Contains(text, "Models that should fit") || strings.Contains(text, "Inference engine:") || strings.Contains(text, "Install it now?") {
		t.Fatalf("worker was prompted:\n%s", text)
	}
	if !strings.Contains(text, "Joining cluster inferoute-cluster1") || !strings.Contains(text, "Qwen3 0.6B (Qwen/Qwen3-0.6B)") {
		t.Fatalf("stdout = %s", text)
	}
	if !strings.Contains(text, "Run this after the master is up") || !strings.Contains(text, docsClusterWorker) {
		t.Fatalf("did not print the worker command:\n%s", text)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Model != "Qwen/Qwen3-0.6B" {
		t.Fatalf("model = %q", cfg.Provider.Model)
	}
}

func TestExecuteYesWritesConfig(t *testing.T) {
	t.Setenv("INFEROUTE_URL", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	catalog := filepath.Join("testdata", "catalog.json")

	var out, errBuf bytes.Buffer
	opts := Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "test-key-123",
		OfflineCatalog: catalog,
		Yes:            true,
		NoStart:        true,
	}
	if err := Execute(opts, Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf}); err != nil {
		t.Fatalf("Execute: %v\nstdout=%s\nstderr=%s", err, out.String(), errBuf.String())
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.APIKey != "test-key-123" {
		t.Errorf("api_key = %q", cfg.Provider.APIKey)
	}
	want := engine.RuntimeKind(runtime.GOOS, engine.KindVLLM)
	if cfg.Provider.Engine != string(want) || cfg.Provider.ProviderType != engine.PlatformType(want) {
		t.Errorf("engine/type = %q/%q, want %s/%s", cfg.Provider.Engine, cfg.Provider.ProviderType, want, engine.PlatformType(want))
	}
	if cfg.Provider.Model != "Qwen/Qwen3-0.6B" {
		t.Errorf("model = %q", cfg.Provider.Model)
	}
	if cfg.Provider.URL != config.DefaultPlatformURL {
		t.Errorf("url = %q", cfg.Provider.URL)
	}
	if !strings.Contains(out.String(), "inferoute-client setup") {
		t.Errorf("stdout should mention re-run: %s", out.String())
	}
}

func TestExecuteYesVLLM(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out, errBuf bytes.Buffer
	opts := Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "k",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}
	if err := Execute(opts, Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf}); err != nil {
		t.Fatalf("Execute: %v\n%s\n%s", err, out.String(), errBuf.String())
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := engine.RuntimeKind(runtime.GOOS, engine.KindVLLM)
	if cfg.Provider.Engine != string(want) || cfg.Provider.ProviderType != engine.PlatformType(want) {
		t.Errorf("engine/type = %q/%q, want %s/%s", cfg.Provider.Engine, cfg.Provider.ProviderType, want, engine.PlatformType(want))
	}
	if cfg.Provider.LLMURL != engine.DefaultURL(want) {
		t.Errorf("llm_url = %q, want %s", cfg.Provider.LLMURL, engine.DefaultURL(want))
	}
}

func TestExecuteYesFreeToken(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out bytes.Buffer
	opts := Options{
		ConfigPath:     cfgPath,
		Engine:         "llama_cpp",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "k",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}
	if err := Execute(opts, Streams{In: strings.NewReader(""), Out: &out, Err: os.Stderr}); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Engine != "llama_cpp" || cfg.Provider.ProviderType != "llama_cpp" {
		t.Errorf("engine/type = %q/%q", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.LLMURL != "http://127.0.0.1:8081" {
		t.Errorf("llm_url = %q", cfg.Provider.LLMURL)
	}
	if cfg.Provider.GGUFFile != "qwen3-0.6b-q4_k_m.gguf" {
		t.Errorf("gguf_file = %q", cfg.Provider.GGUFFile)
	}
}

func TestResolveEngineRemapsServiceTypeVLLM(t *testing.T) {
	tests := []struct {
		goos, in string
		want     engine.Kind
	}{
		{"windows", "vllm", engine.KindLlamaCpp},
		{"darwin", "vllm", engine.KindVLLMMetal},
		{"linux", "vllm", engine.KindVLLM},
		{"windows", "llama_cpp", engine.KindLlamaCpp},
		{"linux", "llama_cpp", engine.KindLlamaCpp},
		{"darwin", "vllm-metal", engine.KindVLLMMetal},
	}
	for _, tt := range tests {
		got, err := resolveEngine(tt.goos, tt.in)
		if err != nil {
			t.Fatalf("resolveEngine(%q, %q): %v", tt.goos, tt.in, err)
		}
		if got != tt.want {
			t.Errorf("resolveEngine(%q, %q) = %s, want %s", tt.goos, tt.in, got, tt.want)
		}
	}
	if _, err := resolveEngine("linux", "nope"); err == nil {
		t.Fatal("expected unknown engine error")
	}
}

func TestExecuteYesVLLMWindowsUsesFreeToken(t *testing.T) {
	orig := hostGOOS
	hostGOOS = "windows"
	t.Cleanup(func() { hostGOOS = orig })
	t.Setenv("LOCALAPPDATA", t.TempDir())

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out, errBuf bytes.Buffer
	opts := Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "k",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}
	if err := Execute(opts, Streams{In: strings.NewReader(""), Out: &out, Err: &errBuf}); err != nil {
		t.Fatalf("Execute: %v\n%s\n%s", err, out.String(), errBuf.String())
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Engine != "llama_cpp" || cfg.Provider.ProviderType != "llama_cpp" {
		t.Errorf("engine/type = %q/%q, want llama_cpp/llama_cpp", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.LLMURL != "http://127.0.0.1:8081" {
		t.Errorf("llm_url = %q, want :8081", cfg.Provider.LLMURL)
	}
}

func TestExecuteYesVLLMWindowsRejectsUntagged(t *testing.T) {
	orig := hostGOOS
	hostGOOS = "windows"
	t.Cleanup(func() { hostGOOS = orig })
	t.Setenv("LOCALAPPDATA", t.TempDir())

	err := Execute(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.yaml"),
		Engine:         "vllm",
		Model:          "baai/bge-m3",
		APIKey:         "k",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}, Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected untagged encoder to be rejected on Windows vllm→llama_cpp")
	}
	if !strings.Contains(err.Error(), "baai/bge-m3") {
		t.Errorf("error = %v", err)
	}
}

func TestExecuteYesRequiresKey(t *testing.T) {
	err := Execute(Options{Yes: true, Engine: "vllm", Model: "x", NoStart: true}, Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteRerunKeepsServerPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	first := Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "first-key",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}
	var out bytes.Buffer
	if err := Execute(first, Streams{In: strings.NewReader(""), Out: &out}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Port = 9090
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	second := first
	second.APIKey = "second-key"
	second.Engine = "vllm"
	second.Model = "Qwen/Qwen3-0.6B"
	out.Reset()
	if err := Execute(second, Streams{In: strings.NewReader(""), Out: &out}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	wantEngine := string(engine.RuntimeKind(runtime.GOOS, engine.KindVLLM))
	if cfg.Provider.APIKey != "second-key" || cfg.Provider.Engine != wantEngine {
		t.Errorf("key/engine = %q/%q, want second-key/%s", cfg.Provider.APIKey, cfg.Provider.Engine, wantEngine)
	}
}

func TestExecuteURLFromEnv(t *testing.T) {
	t.Setenv("INFEROUTE_URL", "https://dev.inferoute.example")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "k",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "https://dev.inferoute.example") {
		t.Errorf("stdout should show override URL: %s", out.String())
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.URL != "https://dev.inferoute.example" {
		t.Errorf("url = %q", cfg.Provider.URL)
	}
}

func TestExecuteCatalogURLFlagBeatsEnv(t *testing.T) {
	t.Setenv("INFEROUTE_URL", "https://env.example")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	var out bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		Model:          "Qwen/Qwen3-0.6B",
		APIKey:         "k",
		CatalogURL:     "https://flag.example",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		Yes:            true,
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.URL != "https://flag.example" {
		t.Errorf("url = %q, want flag", cfg.Provider.URL)
	}
}

func TestPastedJoinCodeSkipsEngineAndModel(t *testing.T) {
	singlePlan(t)
	t.Setenv("PROVIDER_API_KEY", "sk-already-set")
	clusterPlan = func(cfg *config.Config) (clusterSettings, error) {
		if cfg.Provider.APIKey != "join-abc" {
			t.Fatalf("api key = %q", cfg.Provider.APIKey)
		}
		return clusterSettings{
			Role: "worker", Name: "inferoute-cluster1",
			Model: "Qwen/Qwen3-0.6B", DisplayName: "Qwen3 0.6B", ServiceType: "vllm",
		}, nil
	}
	detectEngine = func(kind engine.Kind) engine.Detected {
		return engine.Detected{Kind: kind, Found: true, Bin: "vllm"}
	}
	dir := t.TempDir()
	var out, errBuf bytes.Buffer
	err := Execute(Options{
		ConfigPath:     filepath.Join(dir, "config.yaml"),
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		NoStart:        true,
	}, Streams{In: strings.NewReader("shown once: join-abc\n"), Out: &out, Err: &errBuf})
	if err != nil {
		t.Fatalf("Execute: %v\n%s\n%s", err, out.String(), errBuf.String())
	}
	text := out.String()
	if strings.Contains(text, "Models that should fit") || strings.Contains(text, "Inference engine:") {
		t.Fatalf("join code still asked for engine or model:\n%s", text)
	}
	if !strings.Contains(text, "Joining cluster inferoute-cluster1") || !strings.Contains(text, "Provider API key or join code") {
		t.Fatalf("stdout = %s", text)
	}
}

func TestEnrolledWorkerSkipsKeyPrompt(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{}
	cfg.Provider.APIKey = "join-spent"
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.NodeToken = "node-abc"
	cfg.Provider.Model = "Qwen/Qwen3-0.6B"
	cfg.Provider.Engine = "vllm"
	cfg.Provider.URL = "https://example.test"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	prev := clusterPlan
	t.Cleanup(func() { clusterPlan = prev })
	clusterPlan = func(cfg *config.Config) (clusterSettings, error) {
		if cfg.Provider.NodeToken != "node-abc" || cfg.Provider.APIKey != "join-spent" {
			t.Fatalf("key %q token %q", cfg.Provider.APIKey, cfg.Provider.NodeToken)
		}
		return clusterSettings{Role: "worker", Model: cfg.Provider.Model, ServiceType: "vllm"}, nil
	}
	detectEngine = func(kind engine.Kind) engine.Detected {
		return engine.Detected{Kind: kind, Found: true, Bin: "vllm"}
	}
	var out bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Provider API key or join code") || !strings.Contains(text, "already in the cluster") {
		t.Fatalf("stdout = %s", text)
	}
}

func TestDifferentJoinCodeClearsSavedToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{}
	cfg.Provider.APIKey = "join-spent"
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.NodeToken = "node-abc"
	cfg.Provider.Model = "Qwen/Qwen3-0.6B"
	cfg.Provider.Engine = "vllm"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	prev := clusterPlan
	t.Cleanup(func() { clusterPlan = prev })
	clusterPlan = func(cfg *config.Config) (clusterSettings, error) {
		if cfg.Provider.APIKey != "join-new" || cfg.Provider.NodeToken != "" {
			t.Fatalf("key %q token %q", cfg.Provider.APIKey, cfg.Provider.NodeToken)
		}
		return clusterSettings{Role: "worker", Name: "inferoute-cluster2", Model: "Qwen/Qwen3-0.6B", ServiceType: "vllm"}, nil
	}
	detectEngine = func(kind engine.Kind) engine.Detected {
		return engine.Detected{Kind: kind, Found: true, Bin: "vllm"}
	}
	var out bytes.Buffer
	err := Execute(Options{
		ConfigPath:     cfgPath,
		Engine:         "vllm",
		APIKey:         "join-new",
		OfflineCatalog: filepath.Join("testdata", "catalog.json"),
		NoStart:        true,
	}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	if err != nil {
		t.Fatal(err)
	}
}
