package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/nrfcloud/flux-extension-controller/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRepositoryURL(t *testing.T) {
	// Generate test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := &config.GitHubConfig{
		AppID:        123456,
		Organization: "nrfcloud",
	}

	client := &Client{
		config:     cfg,
		privateKey: privateKey,
	}

	tests := []struct {
		name        string
		repoURL     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid nrfcloud repository",
			repoURL:     "https://github.com/nrfcloud/test-repo",
			expectError: false,
		},
		{
			name:        "valid nrfcloud repository with .git suffix",
			repoURL:     "https://github.com/nrfcloud/test-repo.git",
			expectError: false,
		},
		{
			name:        "invalid URL",
			repoURL:     "invalid-url",
			expectError: true,
			errorMsg:    "repository must be hosted on github.com",
		},
		{
			name:        "non-github host",
			repoURL:     "https://gitlab.com/nrfcloud/test-repo",
			expectError: true,
			errorMsg:    "repository must be hosted on github.com",
		},
		{
			name:        "wrong organization",
			repoURL:     "https://github.com/other-org/test-repo",
			expectError: true,
			errorMsg:    "repository must belong to organization nrfcloud",
		},
		{
			name:        "invalid path",
			repoURL:     "https://github.com/nrfcloud",
			expectError: true,
			errorMsg:    "invalid repository path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ValidateRepositoryURL(tt.repoURL)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseRepositoryURL(t *testing.T) {
	tests := []struct {
		name          string
		repoURL       string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "standard GitHub URL",
			repoURL:       "https://github.com/nrfcloud/test-repo",
			expectedOwner: "nrfcloud",
			expectedRepo:  "test-repo",
			expectError:   false,
		},
		{
			name:          "GitHub URL with .git suffix",
			repoURL:       "https://github.com/nrfcloud/test-repo.git",
			expectedOwner: "nrfcloud",
			expectedRepo:  "test-repo",
			expectError:   false,
		},
		{
			name:        "invalid URL",
			repoURL:     "invalid-url",
			expectError: true,
		},
		{
			name:        "incomplete path",
			repoURL:     "https://github.com/nrfcloud",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRepositoryURL(tt.repoURL)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedOwner, owner)
				assert.Equal(t, tt.expectedRepo, repo)
			}
		})
	}
}

func TestCreateJWT(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := &config.GitHubConfig{
		AppID: 123456,
	}

	client := &Client{
		config:     cfg,
		privateKey: privateKey,
	}

	token, err := client.createJWT()
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Parse and validate the token
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return &privateKey.PublicKey, nil
	})
	require.NoError(t, err)
	assert.True(t, parsedToken.Valid)

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	require.True(t, ok)
	assert.Equal(t, float64(123456), claims["iss"])
	assert.NotNil(t, claims["iat"])
	assert.NotNil(t, claims["exp"])
}

func TestGenerateInstallationToken_Validation(t *testing.T) {
	// Create test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := &config.GitHubConfig{
		AppID:        123456,
		Organization: "nrfcloud",
	}

	client := &Client{
		config:     cfg,
		privateKey: privateKey,
	}

	// Test validation of repository URL before token generation
	err = client.ValidateRepositoryURL("https://github.com/nrfcloud/test-repo")
	assert.NoError(t, err)

	err = client.ValidateRepositoryURL("https://github.com/other-org/test-repo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository must belong to organization nrfcloud")
}

func TestLoadPrivateKey(t *testing.T) {
	// Generate a test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Encode as PEM
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	// Write to temporary file
	tmpFile, err := os.CreateTemp("", "private-key-*.pem")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(privateKeyPEM)
	require.NoError(t, err)
	tmpFile.Close()

	// Test loading the key
	loadedKey, err := loadPrivateKey(tmpFile.Name())
	require.NoError(t, err)
	assert.Equal(t, privateKey.N, loadedKey.N)
	assert.Equal(t, privateKey.E, loadedKey.E)
}

func TestLoadPrivateKey_Errors(t *testing.T) {
	// Test non-existent file
	_, err := loadPrivateKey("/nonexistent/key.pem")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read private key file")

	// Test invalid PEM content
	tmpFile, err := os.CreateTemp("", "invalid-key-*.pem")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("invalid pem content")
	require.NoError(t, err)
	tmpFile.Close()

	_, err = loadPrivateKey(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse private key")
}

func TestJWTTransport(t *testing.T) {
	transport := &jwtTransport{
		token: "test-jwt-token",
	}

	// Create a test server to capture the request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-jwt-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	assert.NoError(t, err)
}
