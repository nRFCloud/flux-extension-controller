package token

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/nrfcloud/flux-extension-controller/pkg/github"
	"github.com/nrfcloud/flux-extension-controller/pkg/kubernetes"
)

// RefreshManager manages token refresh operations
type RefreshManager struct {
	client        client.Client
	githubClient  *github.Client
	secretManager *kubernetes.SecretManager
	logger        logr.Logger

	// Refresh tracking
	refreshJobs  map[string]*RefreshJob
	refreshMutex sync.RWMutex

	refreshInterval time.Duration
	refreshBuffer   time.Duration
}

// RefreshJob represents a scheduled token refresh
type RefreshJob struct {
	SecretNamespace string
	SecretName      string
	RepositoryURL   string
	NextRefresh     time.Time
	Timer           *time.Timer
	Cancel          context.CancelFunc
}

// NewRefreshManager creates a new token refresh manager
func NewRefreshManager(
	client client.Client,
	githubClient *github.Client,
	secretManager *kubernetes.SecretManager,
	refreshInterval time.Duration,
	logger logr.Logger,
) *RefreshManager {
	return &RefreshManager{
		client:          client,
		githubClient:    githubClient,
		secretManager:   secretManager,
		logger:          logger,
		refreshJobs:     make(map[string]*RefreshJob),
		refreshInterval: refreshInterval,
		refreshBuffer:   5 * time.Minute, // Refresh 5 minutes before expiry
	}
}

// ScheduleRefresh schedules a token refresh for the given secret
func (rm *RefreshManager) ScheduleRefresh(ctx context.Context, namespace, name, repositoryURL string) error {
	rm.refreshMutex.Lock()
	defer rm.refreshMutex.Unlock()

	jobKey := fmt.Sprintf("%s/%s", namespace, name)

	// Cancel existing job if it exists
	if existingJob, exists := rm.refreshJobs[jobKey]; exists {
		if existingJob.Cancel != nil {
			existingJob.Cancel()
		}
		if existingJob.Timer != nil {
			existingJob.Timer.Stop()
		}
	}

	// Get current secret to determine refresh time
	secret, err := rm.secretManager.GetSecret(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to get secret for refresh scheduling: %w", err)
	}

	expiry, err := rm.secretManager.GetTokenExpiry(secret)
	if err != nil {
		return fmt.Errorf("failed to get token expiry: %w", err)
	}

	// Calculate next refresh time
	nextRefresh := expiry.Add(-rm.refreshBuffer)
	if nextRefresh.Before(time.Now()) {
		// Token expires soon, refresh immediately
		nextRefresh = time.Now().Add(1 * time.Minute)
	}

	// Create job context
	jobCtx, cancel := context.WithCancel(context.Background())

	// Create refresh job
	job := &RefreshJob{
		SecretNamespace: namespace,
		SecretName:      name,
		RepositoryURL:   repositoryURL,
		NextRefresh:     nextRefresh,
		Cancel:          cancel,
	}

	// Schedule the refresh
	refreshDuration := time.Until(nextRefresh)
	job.Timer = time.AfterFunc(refreshDuration, func() {
		rm.executeRefresh(jobCtx, job)
	})

	rm.refreshJobs[jobKey] = job

	rm.logger.Info("Scheduled token refresh",
		"secret", jobKey,
		"nextRefresh", nextRefresh,
		"refreshIn", refreshDuration)

	return nil
}

// CancelRefresh cancels a scheduled token refresh
func (rm *RefreshManager) CancelRefresh(namespace, name string) {
	rm.refreshMutex.Lock()
	defer rm.refreshMutex.Unlock()

	jobKey := fmt.Sprintf("%s/%s", namespace, name)
	if job, exists := rm.refreshJobs[jobKey]; exists {
		if job.Cancel != nil {
			job.Cancel()
		}
		if job.Timer != nil {
			job.Timer.Stop()
		}
		delete(rm.refreshJobs, jobKey)

		rm.logger.Info("Cancelled token refresh", "secret", jobKey)
	}
}

// executeRefresh performs the actual token refresh
func (rm *RefreshManager) executeRefresh(ctx context.Context, job *RefreshJob) {
	logger := rm.logger.WithValues(
		"secret", fmt.Sprintf("%s/%s", job.SecretNamespace, job.SecretName),
		"repository", job.RepositoryURL,
	)

	logger.Info("Executing token refresh")

	// Validate repository URL
	if err := rm.githubClient.ValidateRepositoryURL(job.RepositoryURL); err != nil {
		logger.Error(err, "Repository URL validation failed")
		return
	}

	// Generate new installation token
	token, err := rm.githubClient.GenerateInstallationToken(ctx, job.RepositoryURL)
	if err != nil {
		logger.Error(err, "Failed to generate installation token")
		return
	}

	// Get the GitRepository object to use as owner
	secret, err := rm.secretManager.GetSecret(ctx, job.SecretNamespace, job.SecretName)
	if err != nil {
		logger.Error(err, "Failed to get secret for owner reference")
		return
	}

	// Find the owner (GitRepository) from the secret's owner references
	var owner client.Object
	for _, ownerRef := range secret.GetOwnerReferences() {
		if ownerRef.Kind == "GitRepository" {
			// For simplicity, we'll use the secret itself as owner
			// In a real implementation, you'd fetch the actual GitRepository object
			owner = secret
			break
		}
	}

	if owner == nil {
		owner = secret // Fallback to secret as owner
	}

	// Update the secret with new token
	if err := rm.secretManager.CreateOrUpdateSecret(
		ctx,
		job.SecretNamespace,
		job.SecretName,
		token,
		job.RepositoryURL,
		owner,
	); err != nil {
		logger.Error(err, "Failed to update secret with new token")
		return
	}

	logger.Info("Token refresh completed successfully")

	// Schedule next refresh
	if err := rm.ScheduleRefresh(ctx, job.SecretNamespace, job.SecretName, job.RepositoryURL); err != nil {
		logger.Error(err, "Failed to schedule next refresh")
	}
}

// CheckAndRefreshExpiredTokens checks all managed secrets and refreshes expired tokens
func (rm *RefreshManager) CheckAndRefreshExpiredTokens(ctx context.Context) error {
	// List all secrets in all namespaces
	secretList := &corev1.SecretList{}
	if err := rm.client.List(ctx, secretList); err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	for _, secret := range secretList.Items {
		if !rm.secretManager.IsSecretManagedByController(&secret) {
			continue
		}

		needsRefresh, err := rm.secretManager.NeedsTokenRefresh(&secret, rm.refreshBuffer)
		if err != nil {
			rm.logger.Error(err, "Failed to check if secret needs refresh",
				"secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name))
			continue
		}

		if needsRefresh {
			repositoryURL := ""
			if secret.Annotations != nil {
				repositoryURL = secret.Annotations[kubernetes.AnnotationRepositoryURL]
			}

			if repositoryURL == "" {
				rm.logger.Error(fmt.Errorf("missing repository URL annotation"),
					"Secret missing repository URL",
					"secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name))
				continue
			}

			if err := rm.ScheduleRefresh(ctx, secret.Namespace, secret.Name, repositoryURL); err != nil {
				rm.logger.Error(err, "Failed to schedule refresh for expired token",
					"secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name))
			}
		}
	}

	return nil
}

// Start starts the refresh manager background processes
func (rm *RefreshManager) Start(ctx context.Context) error {
	rm.logger.Info("Starting token refresh manager")

	// Check for expired tokens on startup
	if err := rm.CheckAndRefreshExpiredTokens(ctx); err != nil {
		rm.logger.Error(err, "Failed to check expired tokens on startup")
	}

	// Start periodic check for expired tokens
	ticker := time.NewTicker(rm.refreshInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				rm.logger.Info("Stopping token refresh manager")
				return
			case <-ticker.C:
				if err := rm.CheckAndRefreshExpiredTokens(ctx); err != nil {
					rm.logger.Error(err, "Failed to check expired tokens")
				}
			}
		}
	}()

	return nil
}

// Stop stops the refresh manager and cancels all scheduled refreshes
func (rm *RefreshManager) Stop() {
	rm.refreshMutex.Lock()
	defer rm.refreshMutex.Unlock()

	rm.logger.Info("Stopping token refresh manager")

	for jobKey, job := range rm.refreshJobs {
		if job.Cancel != nil {
			job.Cancel()
		}
		if job.Timer != nil {
			job.Timer.Stop()
		}
		delete(rm.refreshJobs, jobKey)
	}
}
