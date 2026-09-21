package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
)

func TestExecuteYesWritesConfig(t *testing.T) {
	t.Setenv("INFEROUTE_URL", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	catalog := filepath.Join("testdata", "catalog.json")

	var out, errBuf bytes.Buffer
	opts := Options{
		ConfigPath:     cfgPath,
		Engine:         "ollama",
		Model:          "gguf/qwen3:0.6b",
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
	if cfg.Provider.Engine != "ollama" || cfg.Provider.ProviderType != "ollama" {
		t.Errorf("engine/type = %q/%q", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.Model != "gguf/qwen3:0.6b" {
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
	if cfg.Provider.Engine != string(want) || cfg.Provider.ProviderType != "vllm" {
		t.Errorf("engine/type = %q/%q, want %s/vllm", cfg.Provider.Engine, cfg.Provider.ProviderType, want)
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
		Engine:         "freetoken",
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
	if cfg.Provider.Engine != "freetoken" || cfg.Provider.ProviderType != "vllm" {
		t.Errorf("engine/type = %q/%q", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.LLMURL != "http://127.0.0.1:1919" {
		t.Errorf("llm_url = %q", cfg.Provider.LLMURL)
	}
}

func TestResolveEngineRemapsServiceTypeVLLM(t *testing.T) {
	tests := []struct {
		goos, in string
		want     engine.Kind
	}{
		{"windows", "vllm", engine.KindFreeToken},
		{"darwin", "vllm", engine.KindVLLMMetal},
		{"linux", "vllm", engine.KindVLLM},
		{"windows", "freetoken", engine.KindFreeToken},
		{"windows", "ollama", engine.KindOllama},
		{"linux", "freetoken", engine.KindFreeToken},
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
	if cfg.Provider.Engine != "freetoken" || cfg.Provider.ProviderType != "vllm" {
		t.Errorf("engine/type = %q/%q, want freetoken/vllm", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.LLMURL != "http://127.0.0.1:1919" {
		t.Errorf("llm_url = %q, want :1919", cfg.Provider.LLMURL)
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
		t.Fatal("expected untagged encoder to be rejected on Windows vllm→freetoken")
	}
	if !strings.Contains(err.Error(), "baai/bge-m3") {
		t.Errorf("error = %v", err)
	}
}

func TestExecuteYesRequiresKey(t *testing.T) {
	err := Execute(Options{Yes: true, Engine: "ollama", Model: "x", NoStart: true}, Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteRerunKeepsServerPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	first := Options{
		ConfigPath:     cfgPath,
		Engine:         "ollama",
		Model:          "gguf/qwen3:0.6b",
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
		Engine:         "ollama",
		Model:          "gguf/qwen3:0.6b",
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
		Engine:         "ollama",
		Model:          "gguf/qwen3:0.6b",
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
