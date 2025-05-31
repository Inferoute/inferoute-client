package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Server configuration
	Server struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`

	// Provider configuration
	Provider struct {
		APIKey       string `yaml:"api_key"`
		URL          string `yaml:"url"`
		ProviderType string `yaml:"provider_type"`
		LLMURL       string `yaml:"llm_url"`
	} `yaml:"provider"`

	// Cloudflare configuration
	Cloudflare struct {
		ServiceURL string `yaml:"service_url"`
	} `yaml:"cloudflare"`

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
	cfg.Provider.URL = "http://localhost:80"
	cfg.Provider.ProviderType = "ollama"
	cfg.Provider.LLMURL = "http://localhost:11434"

	// Set default Cloudflare service URL to match LLM URL
	cfg.Cloudflare.ServiceURL = cfg.Provider.LLMURL

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

	// If Cloudflare service URL is not set, default to LLM URL
	if cfg.Cloudflare.ServiceURL == "" {
		cfg.Cloudflare.ServiceURL = cfg.Provider.LLMURL
	}

	return cfg, nil
}
