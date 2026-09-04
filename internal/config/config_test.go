package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultHost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")
	cfg, err := Load(missing)
	if err != nil {
		t.Fatalf("Load(%q) err = %v, want nil", missing, err)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("default Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
}

func TestLoadHostFromYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "omitted host keeps default",
			yaml: "server:\n  port: 9090\n",
			want: "127.0.0.1",
		},
		{
			name: "explicit all-interfaces is kept",
			yaml: "server:\n  host: \"0.0.0.0\"\n",
			want: "0.0.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%q) err = %v, want nil", path, err)
			}
			if cfg.Server.Host != tt.want {
				t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, tt.want)
			}
		})
	}
}

func TestTunnelServiceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{name: "empty", host: "", port: 8080, want: "http://127.0.0.1:8080"},
		{name: "all interfaces", host: "0.0.0.0", port: 8080, want: "http://127.0.0.1:8080"},
		{name: "loopback v4", host: "127.0.0.1", port: 8080, want: "http://127.0.0.1:8080"},
		{name: "localhost alias", host: "localhost", port: 8080, want: "http://127.0.0.1:8080"},
		{name: "loopback v6", host: "::1", port: 9090, want: "http://[::1]:9090"},
		{name: "explicit LAN", host: "192.168.1.10", port: 8080, want: "http://192.168.1.10:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			cfg.Server.Host = tt.host
			cfg.Server.Port = tt.port
			if got := cfg.TunnelServiceURL(); got != tt.want {
				t.Errorf("TunnelServiceURL() host=%q port=%d = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}

func TestLoadEngineDefaultsFromProviderType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		yaml       string
		wantEngine string
		wantType   string
		wantAuto   bool
	}{
		{
			name:       "ollama implied",
			yaml:       "provider:\n  provider_type: ollama\n",
			wantEngine: "ollama",
			wantType:   "ollama",
		},
		{
			name:       "vllm implied",
			yaml:       "provider:\n  provider_type: vllm\n",
			wantEngine: "vllm",
			wantType:   "vllm",
		},
		{
			name:       "freetoken maps platform type to vllm",
			yaml:       "provider:\n  engine: freetoken\n  auto_start: true\n  model: Qwen/Qwen2.5-7B-Instruct\n",
			wantEngine: "freetoken",
			wantType:   "vllm",
			wantAuto:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Provider.Engine != tt.wantEngine {
				t.Errorf("Engine = %q, want %q", cfg.Provider.Engine, tt.wantEngine)
			}
			if cfg.Provider.ProviderType != tt.wantType {
				t.Errorf("ProviderType = %q, want %q", cfg.Provider.ProviderType, tt.wantType)
			}
			if cfg.Provider.AutoStart != tt.wantAuto {
				t.Errorf("AutoStart = %v, want %v", cfg.Provider.AutoStart, tt.wantAuto)
			}
		})
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	in := &Config{}
	in.Server.Port = 8080
	in.Server.Host = "127.0.0.1"
	in.Provider.APIKey = "test-key"
	in.Provider.URL = "https://core.inferoute.com"
	in.Provider.Engine = "freetoken"
	in.Provider.ProviderType = "vllm"
	in.Provider.LLMURL = "http://127.0.0.1:1919"
	in.Provider.Model = "Qwen/Qwen2.5-7B-Instruct"
	in.Provider.AutoStart = true
	in.Provider.EngineBin = `C:\Users\you\AppData\Local\FreeToken\ft.exe`

	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Provider.Engine != "freetoken" || out.Provider.ProviderType != "vllm" {
		t.Errorf("engine/type = %q/%q", out.Provider.Engine, out.Provider.ProviderType)
	}
	if !out.Provider.AutoStart || out.Provider.Model != "Qwen/Qwen2.5-7B-Instruct" {
		t.Errorf("auto_start/model = %v/%q", out.Provider.AutoStart, out.Provider.Model)
	}
	if out.Provider.EngineBin != in.Provider.EngineBin {
		t.Errorf("EngineBin = %q, want %q", out.Provider.EngineBin, in.Provider.EngineBin)
	}
}

func TestHasAPIKey(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	if cfg.HasAPIKey() {
		t.Fatal("empty key should be false")
	}
	cfg.Provider.APIKey = "your_api_key_here"
	if cfg.HasAPIKey() {
		t.Fatal("placeholder should be false")
	}
	cfg.Provider.APIKey = "real"
	if !cfg.HasAPIKey() {
		t.Fatal("real key should be true")
	}
}

func TestPlatformTypeAndDefaultURL(t *testing.T) {
	t.Parallel()
	if got := PlatformType("ollama"); got != "ollama" {
		t.Errorf("PlatformType(ollama) = %q", got)
	}
	if got := PlatformType("freetoken"); got != "vllm" {
		t.Errorf("PlatformType(freetoken) = %q", got)
	}
	if got := DefaultLLMURL("freetoken"); got != "http://127.0.0.1:1919" {
		t.Errorf("DefaultLLMURL(freetoken) = %q", got)
	}
}
