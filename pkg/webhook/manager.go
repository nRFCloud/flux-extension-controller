package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// TokenLength is the length of generated webhook tokens in bytes (64 characters hex)
	TokenLength = 32
	// ManagedByLabel indicates the secret is managed by this controller
	ManagedByLabel = "app.kubernetes.io/managed-by"
	// ControllerName is the name of this controller
	ControllerName = "flux-extension-controller"
)

// Manager handles webhook token generation and secret management
type Manager struct {
	client  client.Client
	baseURL string
}

// NewManager creates a new webhook manager
func NewManager(client client.Client, baseURL string) *Manager {
	return &Manager{
		client:  client,
		baseURL: baseURL,
	}
}

// GenerateToken generates a cryptographically secure random token
func (m *Manager) GenerateToken() (string, error) {
	bytes := make([]byte, TokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// EnsureWebhookSecret creates or retrieves the webhook secret for a receiver
// If the secret doesn't exist, it creates one with a new token
// If the secret exists and is managed by us, it reuses the existing token
// Returns the token value and any error
func (m *Manager) EnsureWebhookSecret(ctx context.Context, namespace, secretName string, owner client.Object) (string, error) {
	secret := &corev1.Secret{}
	err := m.client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      secretName,
	}, secret)

	if err == nil {
		// Secret exists - check if it's managed by us
		if secret.Labels != nil && secret.Labels[ManagedByLabel] == ControllerName {
			// Reuse existing token
			if token, ok := secret.Data["token"]; ok && len(token) > 0 {
				return string(token), nil
			}
		}
		// Secret exists but is not managed by us or has no token
		return "", fmt.Errorf("secret %s/%s exists but is not managed by controller or has no token", namespace, secretName)
	}

	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("failed to get secret: %w", err)
	}

	// Secret doesn't exist - create it with a new token
	token, err := m.GenerateToken()
	if err != nil {
		return "", err
	}

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel: ControllerName,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}

	// Set owner reference if provided
	if owner != nil {
		if err := controllerutil.SetControllerReference(owner, secret, m.client.Scheme()); err != nil {
			return "", fmt.Errorf("failed to set owner reference: %w", err)
		}
	}

	if err := m.client.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("failed to create secret: %w", err)
	}

	return token, nil
}

// ConstructWebhookURL constructs the full webhook URL from base URL and webhook path
func (m *Manager) ConstructWebhookURL(webhookPath string) (string, error) {
	if m.baseURL == "" {
		return "", fmt.Errorf("webhook base URL is not configured")
	}

	baseURL, err := url.Parse(m.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	if baseURL.Scheme != "https" && baseURL.Scheme != "http" {
		return "", fmt.Errorf("base URL must use http or https scheme")
	}

	// Join base URL with webhook path
	webhookURL := baseURL.JoinPath(webhookPath)
	return webhookURL.String(), nil
}

// IsWebhookConfigured returns true if webhook base URL is configured
func (m *Manager) IsWebhookConfigured() bool {
	return m.baseURL != ""
}
