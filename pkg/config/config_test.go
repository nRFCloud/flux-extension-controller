package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_FromFile(t *testing.T) {
	// Create a temporary config file
	configContent := `
github:
  appId: 12345
  installationId: 67890
  privateKeyPath: "/path/to/key"
  organization: "testorg"

controller:
  excludedNamespaces:
    - "test-namespace"
  watchAllNamespaces: false

tokenRefresh:
  refreshInterval: "30m"
  tokenLifetime: "45m"
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	// Test loading config from file
	cfg, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, int64(12345), cfg.GitHub.AppID)
	assert.Equal(t, int64(67890), cfg.GitHub.InstallationID)
	assert.Equal(t, "/path/to/key", cfg.GitHub.PrivateKeyPath)
	assert.Equal(t, "testorg", cfg.GitHub.Organization)
	assert.Equal(t, []string{"test-namespace"}, cfg.Controller.ExcludedNamespaces)
	assert.False(t, cfg.Controller.WatchAllNamespaces)
	assert.Equal(t, 30*time.Minute, cfg.TokenRefresh.RefreshInterval)
	assert.Equal(t, 45*time.Minute, cfg.TokenRefresh.TokenLifetime)
}

func TestLoadConfig_WithEnvironmentVariables(t *testing.T) {
	// Set environment variables
	originalValues := map[string]string{
		"GITHUB_APP_ID":           os.Getenv("GITHUB_APP_ID"),
		"GITHUB_INSTALLATION_ID":  os.Getenv("GITHUB_INSTALLATION_ID"),
		"GITHUB_PRIVATE_KEY_PATH": os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
		"GITHUB_ORGANIZATION":     os.Getenv("GITHUB_ORGANIZATION"),
	}

	// Clean up environment variables after test
	defer func() {
		for key, value := range originalValues {
			if value == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, value)
			}
		}
	}()

	os.Setenv("GITHUB_APP_ID", "67890")
	os.Setenv("GITHUB_INSTALLATION_ID", "11223")
	os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/env/path/to/key")
	os.Setenv("GITHUB_ORGANIZATION", "envorg")

	// Test loading config with environment variables (no file)
	cfg, err := LoadConfig("/nonexistent/config.yaml")
	require.NoError(t, err)

	assert.Equal(t, int64(67890), cfg.GitHub.AppID)
	assert.Equal(t, int64(11223), cfg.GitHub.InstallationID)
	assert.Equal(t, "/env/path/to/key", cfg.GitHub.PrivateKeyPath)
	assert.Equal(t, "envorg", cfg.GitHub.Organization)
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear environment variables
	originalValues := map[string]string{
		"GITHUB_APP_ID":           os.Getenv("GITHUB_APP_ID"),
		"GITHUB_PRIVATE_KEY_PATH": os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
		"GITHUB_ORGANIZATION":     os.Getenv("GITHUB_ORGANIZATION"),
	}

	defer func() {
		for key, value := range originalValues {
			if value == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, value)
			}
		}
	}()

	os.Unsetenv("GITHUB_APP_ID")
	os.Unsetenv("GITHUB_PRIVATE_KEY_PATH")
	os.Unsetenv("GITHUB_ORGANIZATION")

	// Create minimal config with required fields via env vars for validation
	os.Setenv("GITHUB_APP_ID", "123")
	os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/test/key")
	os.Setenv("GITHUB_ORGANIZATION", "testorg")

	cfg, err := LoadConfig("/nonexistent/config.yaml")
	require.NoError(t, err)

	// Test defaults - organization should come from env var now, not default
	assert.Equal(t, "testorg", cfg.GitHub.Organization)
	assert.Equal(t, []string{"flux-system"}, cfg.Controller.ExcludedNamespaces)
	assert.True(t, cfg.Controller.WatchAllNamespaces)
	assert.Equal(t, 50*time.Minute, cfg.TokenRefresh.RefreshInterval)
	assert.Equal(t, 60*time.Minute, cfg.TokenRefresh.TokenLifetime)
}

func TestLoadConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupEnv    func()
		expectedErr string
	}{
		{
			name: "missing app ID",
			setupEnv: func() {
				os.Unsetenv("GITHUB_APP_ID")
				os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/test/key")
				os.Setenv("GITHUB_ORGANIZATION", "testorg")
			},
			expectedErr: "GitHub App ID is required",
		},
		{
			name: "missing private key path",
			setupEnv: func() {
				os.Setenv("GITHUB_APP_ID", "123")
				os.Unsetenv("GITHUB_PRIVATE_KEY_PATH")
				os.Setenv("GITHUB_ORGANIZATION", "testorg")
			},
			expectedErr: "GitHub private key path is required",
		},
		{
			name: "missing organization",
			setupEnv: func() {
				os.Setenv("GITHUB_APP_ID", "123")
				os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/test/key")
				os.Unsetenv("GITHUB_ORGANIZATION")
			},
			expectedErr: "GitHub organization is required",
		},
		{
			name: "invalid app ID",
			setupEnv: func() {
				os.Setenv("GITHUB_APP_ID", "invalid")
				os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/test/key")
				os.Setenv("GITHUB_ORGANIZATION", "testorg")
			},
			expectedErr: "invalid GITHUB_APP_ID",
		},
		{
			name: "invalid installation ID",
			setupEnv: func() {
				os.Setenv("GITHUB_APP_ID", "123")
				os.Setenv("GITHUB_INSTALLATION_ID", "invalid")
				os.Setenv("GITHUB_PRIVATE_KEY_PATH", "/test/key")
				os.Setenv("GITHUB_ORGANIZATION", "testorg")
			},
			expectedErr: "invalid GITHUB_INSTALLATION_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original values
			originalValues := map[string]string{
				"GITHUB_APP_ID":           os.Getenv("GITHUB_APP_ID"),
				"GITHUB_PRIVATE_KEY_PATH": os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
			}

			// Clean up after test
			defer func() {
				for key, value := range originalValues {
					if value == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, value)
					}
				}
			}()

			tt.setupEnv()

			_, err := LoadConfig("/nonexistent/config.yaml")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
