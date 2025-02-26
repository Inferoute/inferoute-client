package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Server configuration
	Server struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`

	// Ollama configuration
	Ollama struct {
		URL string `yaml:"url"`
	} `yaml:"ollama"`

	// Provider configuration
	Provider struct {
		APIKey string `yaml:"api_key"`
		URL    string `yaml:"url"`
	} `yaml:"provider"`

	// NGROK configuration (hardcoded for now)
	NGROK struct {
		URL string `yaml:"url"`
	} `yaml:"ngrok"`
}

// Load loads the configuration from a YAML file
func Load(path string) (*Config, error) {
	// Create default configuration
	cfg := &Config{}

	// Set default values
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	cfg.Ollama.URL = "http://localhost:11434"
	cfg.Provider.URL = "http://localhost:80"

	// Hardcode NGROK URL as specified
	cfg.NGROK.URL = "https://6a2f-2a02-c7c-a0c9-5000-127c-61ff-fe4b-7035.ngrok-free.app"

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
