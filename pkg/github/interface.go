package github

import (
	"context"

	"github.com/google/go-github/v74/github"
)

// GitHubClient interface defines the methods needed for GitHub operations
type GitHubClient interface {
	ValidateRepositoryURL(repoURL string) error
	GenerateInstallationToken(ctx context.Context, repoURL string) (*github.InstallationToken, error)
}

// Ensure Client implements GitHubClient interface
var _ GitHubClient = (*Client)(nil)
