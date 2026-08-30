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
		{name: "empty", host: "", port: 8080, want: "http://localhost:8080"},
		{name: "all interfaces", host: "0.0.0.0", port: 8080, want: "http://localhost:8080"},
		{name: "loopback v4", host: "127.0.0.1", port: 8080, want: "http://localhost:8080"},
		{name: "loopback v6", host: "::1", port: 9090, want: "http://localhost:9090"},
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
