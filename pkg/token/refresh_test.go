package token

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v76/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/nrfcloud/flux-extension-controller/pkg/kubernetes"
)

// MockGitHubClient is a mock implementation of the GitHub client
type MockGitHubClient struct {
	mock.Mock
}

func (m *MockGitHubClient) ValidateRepositoryURL(repoURL string) error {
	args := m.Called(repoURL)
	return args.Error(0)
}

func (m *MockGitHubClient) GenerateInstallationToken(ctx context.Context, repoURL string) (*github.InstallationToken, error) {
	args := m.Called(ctx, repoURL)
	return args.Get(0).(*github.InstallationToken), args.Error(1)
}

func TestRefreshManager_ScheduleRefresh(t *testing.T) {
	s := scheme.Scheme

	// Create a secret with token expiry
	expiresAt := time.Now().Add(1 * time.Hour)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				kubernetes.AnnotationManagedBy:     "flux-extension-controller",
				kubernetes.AnnotationTokenExpiry:   expiresAt.Format(time.RFC3339),
				kubernetes.AnnotationRepositoryURL: "https://github.com/testorg/test-repo",
			},
		},
		Data: map[string][]byte{
			"username": []byte("git"),
			"password": []byte("test-token"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		30*time.Minute,
		logger,
	)

	ctx := context.Background()
	err := refreshManager.ScheduleRefresh(ctx, "test-namespace", "test-secret", "https://github.com/testorg/test-repo")
	require.NoError(t, err)

	// Verify job was scheduled
	refreshManager.refreshMutex.RLock()
	jobKey := "test-namespace/test-secret"
	job, exists := refreshManager.refreshJobs[jobKey]
	refreshManager.refreshMutex.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, job)
	assert.Equal(t, "test-namespace", job.SecretNamespace)
	assert.Equal(t, "test-secret", job.SecretName)
	assert.Equal(t, "https://github.com/testorg/test-repo", job.RepositoryURL)
	assert.NotNil(t, job.Timer)
	assert.NotNil(t, job.Cancel)

	// Clean up
	refreshManager.CancelRefresh("test-namespace", "test-secret")
}

func TestRefreshManager_CancelRefresh(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		30*time.Minute,
		logger,
	)

	// Manually add a job to test cancellation
	jobKey := "test-namespace/test-secret"
	jobCtx, cancel := context.WithCancel(context.Background())
	timer := time.NewTimer(1 * time.Hour)

	refreshManager.refreshMutex.Lock()
	refreshManager.refreshJobs[jobKey] = &RefreshJob{
		SecretNamespace: "test-namespace",
		SecretName:      "test-secret",
		Timer:           timer,
		Cancel:          cancel,
	}
	refreshManager.refreshMutex.Unlock()

	// Cancel the refresh
	refreshManager.CancelRefresh("test-namespace", "test-secret")

	// Verify job was removed
	refreshManager.refreshMutex.RLock()
	_, exists := refreshManager.refreshJobs[jobKey]
	refreshManager.refreshMutex.RUnlock()

	assert.False(t, exists)

	// Clean up context
	_ = jobCtx
}

func TestRefreshManager_CheckAndRefreshExpiredTokens(t *testing.T) {
	s := scheme.Scheme

	// Create secrets with different expiry times
	soonExpires := time.Now().Add(2 * time.Minute)
	laterExpires := time.Now().Add(30 * time.Minute)

	secrets := []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "expires-soon",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					kubernetes.AnnotationManagedBy:     "flux-extension-controller",
					kubernetes.AnnotationTokenExpiry:   soonExpires.Format(time.RFC3339),
					kubernetes.AnnotationRepositoryURL: "https://github.com/testorg/test-repo-1",
				},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "expires-later",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					kubernetes.AnnotationManagedBy:     "flux-extension-controller",
					kubernetes.AnnotationTokenExpiry:   laterExpires.Format(time.RFC3339),
					kubernetes.AnnotationRepositoryURL: "https://github.com/testorg/test-repo-2",
				},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "not-managed",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					kubernetes.AnnotationManagedBy: "other-controller",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secrets...).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		30*time.Minute,
		logger,
	)

	ctx := context.Background()
	err := refreshManager.CheckAndRefreshExpiredTokens(ctx)
	require.NoError(t, err)

	// Verify that only the soon-expiring secret got scheduled for refresh
	refreshManager.refreshMutex.RLock()
	jobs := refreshManager.refreshJobs
	refreshManager.refreshMutex.RUnlock()

	// Should have scheduled refresh for the soon-expiring secret
	soonExpiresKey := "test-namespace/expires-soon"
	_, soonExpiresScheduled := jobs[soonExpiresKey]
	assert.True(t, soonExpiresScheduled)

	// Should not have scheduled refresh for the later-expiring secret
	laterExpiresKey := "test-namespace/expires-later"
	_, laterExpiresScheduled := jobs[laterExpiresKey]
	assert.False(t, laterExpiresScheduled)

	// Clean up
	for jobKey := range jobs {
		if job, exists := jobs[jobKey]; exists {
			if job.Cancel != nil {
				job.Cancel()
			}
			if job.Timer != nil {
				job.Timer.Stop()
			}
		}
	}
}

func TestRefreshManager_executeRefresh(t *testing.T) {
	s := scheme.Scheme

	// Create a secret that needs refresh
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				kubernetes.AnnotationManagedBy:     "flux-extension-controller",
				kubernetes.AnnotationTokenExpiry:   time.Now().Add(5 * time.Minute).Format(time.RFC3339),
				kubernetes.AnnotationRepositoryURL: "https://github.com/testorg/test-repo",
			},
		},
		Data: map[string][]byte{
			"username": []byte("git"),
			"password": []byte("old-token"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		30*time.Minute,
		logger,
	)

	// Set up mock expectations
	repoURL := "https://github.com/testorg/test-repo"
	newExpiresAt := time.Now().Add(1 * time.Hour)
	newToken := &github.InstallationToken{
		Token:     github.String("new-refreshed-token"),
		ExpiresAt: &github.Timestamp{Time: newExpiresAt},
	}

	mockGitHubClient.On("ValidateRepositoryURL", repoURL).Return(nil)
	mockGitHubClient.On("GenerateInstallationToken", mock.Anything, repoURL).Return(newToken, nil)

	// Create refresh job
	job := &RefreshJob{
		SecretNamespace: "test-namespace",
		SecretName:      "test-secret",
		RepositoryURL:   repoURL,
	}

	// Execute refresh
	ctx := context.Background()
	refreshManager.executeRefresh(ctx, job)

	// Verify the secret was updated
	updatedSecret := &corev1.Secret{}
	err := fakeClient.Get(ctx, client.ObjectKey{
		Namespace: "test-namespace",
		Name:      "test-secret",
	}, updatedSecret)
	require.NoError(t, err)

	assert.Equal(t, []byte("new-refreshed-token"), updatedSecret.Data["password"])
	assert.Equal(t, newExpiresAt.Format(time.RFC3339), updatedSecret.Annotations[kubernetes.AnnotationTokenExpiry])

	// Verify mock expectations
	mockGitHubClient.AssertExpectations(t)
}

func TestRefreshManager_executeRefresh_ValidationFailure(t *testing.T) {
	s := scheme.Scheme

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		30*time.Minute,
		logger,
	)

	// Set up mock to return validation error
	repoURL := "https://github.com/testorg/test-repo"
	mockGitHubClient.On("ValidateRepositoryURL", repoURL).Return(assert.AnError)

	job := &RefreshJob{
		SecretNamespace: "test-namespace",
		SecretName:      "test-secret",
		RepositoryURL:   repoURL,
	}

	// Execute refresh - should handle validation error gracefully
	ctx := context.Background()
	refreshManager.executeRefresh(ctx, job)

	// Verify mock was called
	mockGitHubClient.AssertExpectations(t)
}

func TestRefreshManager_Start_And_Stop(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	mockGitHubClient := &MockGitHubClient{}
	secretManager := kubernetes.NewSecretManager(fakeClient)
	logger := logr.Discard()

	refreshManager := NewRefreshManager(
		fakeClient,
		mockGitHubClient,
		secretManager,
		1*time.Second, // Short interval for testing
		logger,
	)

	// Start the refresh manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := refreshManager.Start(ctx)
	require.NoError(t, err)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop the refresh manager
	refreshManager.Stop()

	// Verify all jobs are cleaned up
	refreshManager.refreshMutex.RLock()
	jobCount := len(refreshManager.refreshJobs)
	refreshManager.refreshMutex.RUnlock()

	assert.Equal(t, 0, jobCount)
}
