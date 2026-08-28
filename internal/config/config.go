package config

import (
	"fmt"
	"os"
	"path/filepath"
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
		LLMURL       string `yaml:"llm_url"`
		HFHubCache   string `yaml:"hf_hub_cache"` // optional; default ~/.cache/huggingface/hub
		ModelPath    string `yaml:"model_path"`   // optional flat dir override (hf download --local-dir)
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
	cfg.Server.Host = "0.0.0.0"
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
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
	}

	return cfg, nil
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
// Uses localhost when Server.Host is 0.0.0.0 so cloudflared connects to the proxy on the same machine.
func (c *Config) TunnelServiceURL() string {
	host := c.Server.Host
	if host == "0.0.0.0" || host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d", host, c.Server.Port)
}

// LocalDashboardURL is the loopback URL for the in-process status page.
func (c *Config) LocalDashboardURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", c.Server.Port)
}
