package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Server configuration
	Server struct {
		Port                   int    `yaml:"port"`
		Host                   string `yaml:"host"`
		MaxConcurrentInference int    `yaml:"max_concurrent_inference"` // 0 = unlimited; default 1
		// SessionQueueWaitSeconds bounds how long a same-session request
		// (matching X-Session-Key) waits for the inference slot instead of
		// being rejected with 503. Default 90.
		SessionQueueWaitSeconds int `yaml:"session_queue_wait_seconds"`
		// RequestTimeoutSeconds sets the HTTP server read/write timeouts.
		// Must cover the session queue wait plus inference so the
		// orchestrator's sticky timeout (default 120s) is usable. Default 240.
		RequestTimeoutSeconds int `yaml:"request_timeout_seconds"`
	} `yaml:"server"`

	// Provider configuration
	Provider struct {
		APIKey       string `yaml:"api_key"`
		URL          string `yaml:"url"`
		ProviderType string `yaml:"provider_type"`
		// Engine is the local inference binary: ollama, vllm, vllm-metal, freetoken.
		// Empty defaults from ProviderType so existing configs keep working.
		Engine string `yaml:"engine,omitempty"`
		// EngineBin is an absolute path when the binary is not on PATH (Windows FreeToken).
		EngineBin string `yaml:"engine_bin,omitempty"`
		LLMURL    string `yaml:"llm_url"`
		// Model is the catalog alias (or HF repo) used to start the engine.
		Model string `yaml:"model,omitempty"`
		// AutoStart, when true, starts the engine if llm_url is down.
		AutoStart  bool   `yaml:"auto_start"`
		HFHubCache string `yaml:"hf_hub_cache,omitempty"` // optional; default ~/.cache/huggingface/hub
		ModelPath  string `yaml:"model_path,omitempty"`   // optional flat dir override (hf download --local-dir)
		// LLMTimeoutSeconds is the timeout for requests forwarded to the
		// local vLLM/Ollama instance. Default 120, aligned with the
		// orchestrator's sticky inference timeout.
		LLMTimeoutSeconds int `yaml:"llm_timeout_seconds"`
	} `yaml:"provider"`

	// Logging configuration
	Logging logger.Config `yaml:"logging"`
}

// Load loads the configuration from a YAML file
func Load(path string) (*Config, error) {
	// Create default configuration
	cfg := &Config{}

	// Set default values
	cfg.Server.Port = 8080
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.MaxConcurrentInference = 1
	cfg.Server.SessionQueueWaitSeconds = 90
	cfg.Server.RequestTimeoutSeconds = 240
	cfg.Provider.URL = "http://localhost:80"
	cfg.Provider.ProviderType = "ollama"
	cfg.Provider.LLMURL = "http://localhost:11434"
	cfg.Provider.LLMTimeoutSeconds = 120

	// Set default logging configuration
	homeDir, err := os.UserHomeDir()
	if err == nil {
		cfg.Logging.LogDir = filepath.Join(homeDir, ".local", "state", "inferoute", "log")
	}
	cfg.Logging.Level = "info"
	cfg.Logging.MaxSize = 100
	cfg.Logging.MaxBackups = 5
	cfg.Logging.MaxAge = 30

	// Read configuration file
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist, use default configuration
		if os.IsNotExist(err) {
			fmt.Printf("Configuration file %s not found, using defaults\n", path)
			cfg.normalize()
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
	}

	cfg.normalize()
	return cfg, nil
}

func (c *Config) normalize() {
	c.Provider.ProviderType = strings.ToLower(strings.TrimSpace(c.Provider.ProviderType))
	c.Provider.Engine = strings.ToLower(strings.TrimSpace(c.Provider.Engine))
	c.Provider.EngineBin = strings.TrimSpace(c.Provider.EngineBin)
	c.Provider.Model = strings.TrimSpace(c.Provider.Model)
	if c.Provider.Engine != "" {
		c.Provider.ProviderType = PlatformType(c.Provider.Engine)
		return
	}
	switch c.Provider.ProviderType {
	case "vllm":
		c.Provider.Engine = "vllm"
	default:
		c.Provider.Engine = "ollama"
		if c.Provider.ProviderType == "" {
			c.Provider.ProviderType = "ollama"
		}
	}
}

// PlatformType is the Inferoute provider_type for a local engine.
func PlatformType(engine string) string {
	if strings.EqualFold(strings.TrimSpace(engine), "ollama") {
		return "ollama"
	}
	return "vllm"
}

// DefaultLLMURL is the loopback URL for an engine.
func DefaultLLMURL(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "ollama":
		return "http://127.0.0.1:11434"
	case "freetoken":
		return "http://127.0.0.1:1919"
	default:
		return "http://127.0.0.1:8000"
	}
}

// DefaultPath is ~/.config/inferoute/config.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "inferoute", "config.yaml"), nil
}

// HasAPIKey reports whether api_key is set to a real value.
func (c *Config) HasAPIKey() bool {
	k := strings.TrimSpace(c.Provider.APIKey)
	return k != "" && k != "your_api_key_here"
}

// Save writes cfg to path, creating the parent directory.
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal configuration: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write configuration file: %w", err)
	}
	return nil
}

// SessionQueueWait is the bounded wait for same-session queued requests.
func (c *Config) SessionQueueWait() time.Duration {
	return time.Duration(c.Server.SessionQueueWaitSeconds) * time.Second
}

// RequestTimeout is the HTTP server read/write timeout.
func (c *Config) RequestTimeout() time.Duration {
	return time.Duration(c.Server.RequestTimeoutSeconds) * time.Second
}

// LLMTimeout is the timeout for requests forwarded to the local LLM.
func (c *Config) LLMTimeout() time.Duration {
	return time.Duration(c.Provider.LLMTimeoutSeconds) * time.Second
}

// TunnelServiceURL returns the URL the Cloudflare tunnel should target (the proxy).
// Loopback and all-interfaces binds are rewritten to 127.0.0.1 — not "localhost".
// macOS resolves localhost to ::1 first; the server listens on 127.0.0.1, so
// cloudflared then 502s. Linux/Windows typically hit IPv4 and hide the bug.
func (c *Config) TunnelServiceURL() string {
	host := c.Server.Host
	switch host {
	case "", "0.0.0.0", "127.0.0.1", "localhost":
		return fmt.Sprintf("http://127.0.0.1:%d", c.Server.Port)
	case "::1":
		return fmt.Sprintf("http://[::1]:%d", c.Server.Port)
	}
	return fmt.Sprintf("http://%s:%d", host, c.Server.Port)
}

// LocalDashboardURL is the loopback URL for the in-process status page.
func (c *Config) LocalDashboardURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", c.Server.Port)
}
