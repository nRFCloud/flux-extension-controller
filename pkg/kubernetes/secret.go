package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/google/go-github/v57/github"
)

const (
	// SecretTypeGitRepository is the type for git repository secrets
	SecretTypeGitRepository = "kubernetes.io/git-repository"

	// AnnotationManagedBy indicates the secret is managed by this controller
	AnnotationManagedBy = "flux-extension-controller.nrfcloud.com/managed-by"

	// AnnotationTokenExpiry stores the token expiration time
	AnnotationTokenExpiry = "flux-extension-controller.nrfcloud.com/token-expiry"

	// AnnotationRepositoryURL stores the repository URL
	AnnotationRepositoryURL = "flux-extension-controller.nrfcloud.com/repository-url"
)

// SecretManager handles Kubernetes secret operations for Git repositories
type SecretManager struct {
	client client.Client
}

// NewSecretManager creates a new secret manager
func NewSecretManager(client client.Client) *SecretManager {
	return &SecretManager{
		client: client,
	}
}

// CreateOrUpdateSecret creates or updates a Git repository secret with the GitHub token
func (sm *SecretManager) CreateOrUpdateSecret(
	ctx context.Context,
	namespace, name string,
	token *github.InstallationToken,
	repositoryURL string,
	owner metav1.Object,
) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, sm.client, secret, func() error {
		// Set secret type
		secret.Type = SecretTypeGitRepository

		// Set data
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["username"] = []byte("git")
		secret.Data["password"] = []byte(token.GetToken())

		// Set annotations
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		secret.Annotations[AnnotationManagedBy] = "flux-extension-controller"
		secret.Annotations[AnnotationTokenExpiry] = token.GetExpiresAt().Format(time.RFC3339)
		secret.Annotations[AnnotationRepositoryURL] = repositoryURL

		// Set owner reference
		return controllerutil.SetControllerReference(owner, secret, sm.client.Scheme())
	})

	if err != nil {
		return fmt.Errorf("failed to create or update secret: %w", err)
	}

	if op == controllerutil.OperationResultCreated {
		fmt.Printf("Created secret %s/%s\n", namespace, name)
	} else if op == controllerutil.OperationResultUpdated {
		fmt.Printf("Updated secret %s/%s\n", namespace, name)
	}

	return nil
}

// GetSecret retrieves a secret by name and namespace
func (sm *SecretManager) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := sm.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, secret)

	if err != nil {
		return nil, err
	}

	return secret, nil
}

// IsSecretManagedByController checks if a secret is managed by this controller
func (sm *SecretManager) IsSecretManagedByController(secret *corev1.Secret) bool {
	if secret.Annotations == nil {
		return false
	}

	managedBy, exists := secret.Annotations[AnnotationManagedBy]
	return exists && managedBy == "flux-extension-controller"
}

// GetTokenExpiry returns the token expiration time from secret annotations
func (sm *SecretManager) GetTokenExpiry(secret *corev1.Secret) (time.Time, error) {
	if secret.Annotations == nil {
		return time.Time{}, fmt.Errorf("secret has no annotations")
	}

	expiryStr, exists := secret.Annotations[AnnotationTokenExpiry]
	if !exists {
		return time.Time{}, fmt.Errorf("secret has no token expiry annotation")
	}

	expiry, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse token expiry: %w", err)
	}

	return expiry, nil
}

// NeedsTokenRefresh checks if the secret's token needs to be refreshed
func (sm *SecretManager) NeedsTokenRefresh(secret *corev1.Secret, refreshThreshold time.Duration) (bool, error) {
	if !sm.IsSecretManagedByController(secret) {
		return false, nil
	}

	expiry, err := sm.GetTokenExpiry(secret)
	if err != nil {
		return true, err // If we can't determine expiry, assume it needs refresh
	}

	return time.Until(expiry) < refreshThreshold, nil
}

// ValidateSecretOwnership checks if a secret can be managed by this controller
func (sm *SecretManager) ValidateSecretOwnership(ctx context.Context, namespace, name string, repositoryURL string) error {
	secret, err := sm.GetSecret(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		// Secret doesn't exist, we can create it
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	// Check if it's managed by this controller
	if !sm.IsSecretManagedByController(secret) {
		return fmt.Errorf("secret %s/%s exists but is not managed by flux-extension-controller", namespace, name)
	}

	// Check if it's for the same repository
	if secret.Annotations != nil {
		if existingURL, exists := secret.Annotations[AnnotationRepositoryURL]; exists {
			if existingURL != repositoryURL {
				return fmt.Errorf("secret %s/%s is managed by controller but for different repository: %s", namespace, name, existingURL)
			}
		}
	}

	return nil
}
