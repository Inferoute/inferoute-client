package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
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
	if cfg.Provider.Engine != "vllm" || cfg.Provider.ProviderType != "vllm" {
		t.Errorf("engine/type = %q/%q", cfg.Provider.Engine, cfg.Provider.ProviderType)
	}
	if cfg.Provider.LLMURL != "http://127.0.0.1:8000" {
		t.Errorf("llm_url = %q", cfg.Provider.LLMURL)
	}
}

func TestExecuteYesFreeToken(t *testing.T) {
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
	if cfg.Provider.APIKey != "second-key" || cfg.Provider.Engine != "vllm" {
		t.Errorf("key/engine = %q/%q", cfg.Provider.APIKey, cfg.Provider.Engine)
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
