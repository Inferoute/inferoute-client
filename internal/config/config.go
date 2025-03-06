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

	// NGROK configuration (hardcoded for now)
	NGROK struct {
		URL string `yaml:"url"`
	} `yaml:"ngrok"`

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

	// Hardcode NGROK URL as specified
	cfg.NGROK.URL = "https://6a2f-2a02-c7c-a0c9-5000-127c-61ff-fe4b-7035.ngrok-free.app"

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
