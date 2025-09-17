package github

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v57/github"
	"github.com/nrfcloud/flux-extension-controller/pkg/config"
)

// Client wraps the GitHub client with App authentication
type Client struct {
	client     *github.Client
	config     *config.GitHubConfig
	privateKey *rsa.PrivateKey
}

// NewClient creates a new GitHub client with App authentication
func NewClient(cfg *config.GitHubConfig) (*Client, error) {
	privateKey, err := loadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load private key: %w", err)
	}

	client := github.NewClient(nil)

	return &Client{
		client:     client,
		config:     cfg,
		privateKey: privateKey,
	}, nil
}

// ValidateRepositoryURL checks if the repository URL belongs to the configured organization
func (c *Client) ValidateRepositoryURL(repoURL string) error {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	if parsedURL.Host != "github.com" {
		return fmt.Errorf("repository must be hosted on github.com")
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if len(pathParts) < 2 {
		return fmt.Errorf("invalid repository path")
	}

	org := pathParts[0]
	if org != c.config.Organization {
		return fmt.Errorf("repository must belong to organization %s, got %s", c.config.Organization, org)
	}

	return nil
}

// GenerateInstallationToken creates an installation token for the repository
func (c *Client) GenerateInstallationToken(ctx context.Context, repoURL string) (*github.InstallationToken, error) {
	// Parse repository from URL
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Create JWT for App authentication
	token, err := c.createJWT()
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT: %w", err)
	}

	// Create a new client with JWT authentication
	jwtClient := github.NewClient(&http.Client{
		Transport: &jwtTransport{
			token: token,
		},
	})

	var installationID int64

	// Use configured installation ID if provided, otherwise find it dynamically
	if c.config.InstallationID != 0 {
		installationID = c.config.InstallationID
	} else {
		// Find installation for the repository
		installation, err := c.findInstallation(ctx, owner, repo, jwtClient)
		if err != nil {
			return nil, fmt.Errorf("failed to find installation: %w", err)
		}
		installationID = installation.GetID()
	}

	// Create installation token
	installationToken, _, err := jwtClient.Apps.CreateInstallationToken(
		ctx,
		installationID,
		&github.InstallationTokenOptions{
			Repositories: []string{repo},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create installation token: %w", err)
	}

	return installationToken, nil
}

// createJWT creates a JWT token for GitHub App authentication
func (c *Client) createJWT() (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": c.config.AppID,
	})

	tokenString, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return tokenString, nil
}

// findInstallation finds the GitHub App installation for the given repository
func (c *Client) findInstallation(ctx context.Context, owner, repo string, client *github.Client) (*github.Installation, error) {
	installation, _, err := client.Apps.FindRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to find repository installation: %w", err)
	}

	return installation, nil
}

// parseRepositoryURL extracts owner and repository name from GitHub URL
func parseRepositoryURL(repoURL string) (string, string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if len(pathParts) < 2 {
		return "", "", fmt.Errorf("invalid repository path")
	}

	owner := pathParts[0]
	repo := strings.TrimSuffix(pathParts[1], ".git")

	return owner, repo, nil
}

// loadPrivateKey loads the RSA private key from file
func loadPrivateKey(keyPath string) (*rsa.PrivateKey, error) {
	keyData, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return key, nil
}

// jwtTransport implements http.RoundTripper for JWT authentication
type jwtTransport struct {
	token string
}

func (t *jwtTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	return http.DefaultTransport.RoundTrip(req)
}
