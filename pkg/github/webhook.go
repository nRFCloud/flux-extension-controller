package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v76/github"
)

// WebhookManager handles GitHub webhook operations
type WebhookManager struct {
	*Client
}

// NewWebhookManager creates a new webhook manager
func NewWebhookManager(client *Client) *WebhookManager {
	return &WebhookManager{Client: client}
}

// getAuthenticatedClient creates an authenticated GitHub client for the repository
func (w *WebhookManager) getAuthenticatedClient(ctx context.Context, owner, repo string) (*github.Client, error) {
	// Create JWT for App authentication
	token, err := w.createJWT()
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT: %w", err)
	}

	jwtClient := github.NewClient(&http.Client{
		Transport: &jwtTransport{
			token: token,
		},
	})

	// Find or use configured installation ID
	var installationID int64
	if w.config.InstallationID != 0 {
		installationID = w.config.InstallationID
	} else {
		installation, err := w.findInstallation(ctx, owner, repo, jwtClient)
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

	// Return authenticated client with installation token
	return github.NewClient(nil).WithAuthToken(installationToken.GetToken()), nil
}

// CreateWebhook creates a new webhook in the GitHub repository
func (w *WebhookManager) CreateWebhook(ctx context.Context, repoURL, webhookURL, secret string, events []string) (*github.Hook, error) {
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Get authenticated client
	authClient, err := w.getAuthenticatedClient(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	// Create webhook
	hook := &github.Hook{
		Name:   github.String("web"),
		Active: github.Bool(true),
		Events: events,
		Config: &github.HookConfig{
			URL:         github.String(webhookURL),
			ContentType: github.String("json"),
			Secret:      github.String(secret),
		},
	}

	createdHook, _, err := authClient.Repositories.CreateHook(ctx, owner, repo, hook)
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	return createdHook, nil
}

// GetWebhook retrieves a webhook by ID
func (w *WebhookManager) GetWebhook(ctx context.Context, repoURL string, hookID int64) (*github.Hook, error) {
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Get authenticated client
	authClient, err := w.getAuthenticatedClient(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	hook, _, err := authClient.Repositories.GetHook(ctx, owner, repo, hookID)
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}

	return hook, nil
}

// UpdateWebhook updates an existing webhook
func (w *WebhookManager) UpdateWebhook(ctx context.Context, repoURL string, hookID int64, webhookURL, secret string, events []string) (*github.Hook, error) {
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Get authenticated client
	authClient, err := w.getAuthenticatedClient(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	// Update webhook
	hook := &github.Hook{
		Active: github.Bool(true),
		Events: events,
		Config: &github.HookConfig{
			URL:         github.String(webhookURL),
			ContentType: github.String("json"),
			Secret:      github.String(secret),
		},
	}

	updatedHook, _, err := authClient.Repositories.EditHook(ctx, owner, repo, hookID, hook)
	if err != nil {
		return nil, fmt.Errorf("failed to update webhook: %w", err)
	}

	return updatedHook, nil
}

// DeleteWebhook deletes a webhook
func (w *WebhookManager) DeleteWebhook(ctx context.Context, repoURL string, hookID int64) error {
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		return fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Get authenticated client
	authClient, err := w.getAuthenticatedClient(ctx, owner, repo)
	if err != nil {
		return err
	}

	_, err = authClient.Repositories.DeleteHook(ctx, owner, repo, hookID)
	if err != nil {
		return fmt.Errorf("failed to delete webhook: %w", err)
	}

	return nil
}
