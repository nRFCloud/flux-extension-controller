package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

// Config holds the controller configuration
type Config struct {
	GitHub       GitHubConfig       `yaml:"github"`
	Controller   ControllerConfig   `yaml:"controller"`
	TokenRefresh TokenRefreshConfig `yaml:"tokenRefresh"`
}

// GitHubConfig holds GitHub App configuration
type GitHubConfig struct {
	AppID          int64  `yaml:"appId"`
	PrivateKeyPath string `yaml:"privateKeyPath"`
	Organization   string `yaml:"organization"`
}

// ControllerConfig holds controller-specific configuration
type ControllerConfig struct {
	ExcludedNamespaces []string `yaml:"excludedNamespaces"`
	WatchAllNamespaces bool     `yaml:"watchAllNamespaces"`
}

// TokenRefreshConfig holds token refresh configuration
type TokenRefreshConfig struct {
	RefreshInterval time.Duration `yaml:"refreshInterval"`
	TokenLifetime   time.Duration `yaml:"tokenLifetime"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
	cfg := &Config{
		GitHub: GitHubConfig{
			Organization: "nrfcloud",
		},
		Controller: ControllerConfig{
			ExcludedNamespaces: []string{"flux-system"},
			WatchAllNamespaces: true,
		},
		TokenRefresh: TokenRefreshConfig{
			RefreshInterval: 50 * time.Minute,
			TokenLifetime:   60 * time.Minute,
		},
	}

	// Load from file if it exists
	if _, err := os.Stat(configPath); err == nil {
		data, err := ioutil.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Override with environment variables
	if appID := os.Getenv("GITHUB_APP_ID"); appID != "" {
		var id int64
		if _, err := fmt.Sscanf(appID, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
		}
		cfg.GitHub.AppID = id
	}

	if privateKeyPath := os.Getenv("GITHUB_PRIVATE_KEY_PATH"); privateKeyPath != "" {
		cfg.GitHub.PrivateKeyPath = privateKeyPath
	}

	if organization := os.Getenv("GITHUB_ORGANIZATION"); organization != "" {
		cfg.GitHub.Organization = organization
	}

	// Validate required fields
	if cfg.GitHub.AppID == 0 {
		return nil, fmt.Errorf("GitHub App ID is required")
	}

	if cfg.GitHub.PrivateKeyPath == "" {
		return nil, fmt.Errorf("GitHub private key path is required")
	}

	return cfg, nil
}
