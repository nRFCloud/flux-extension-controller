package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateToken(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	manager := NewManager(client, "https://example.com")

	// Generate multiple tokens and ensure they're unique and correct length
	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		token, err := manager.GenerateToken()
		require.NoError(t, err)
		assert.Len(t, token, TokenLength*2) // hex encoding doubles the length
		assert.NotContains(t, tokens, token, "tokens should be unique")
		tokens[token] = true
	}
}

func TestEnsureWebhookSecret_CreateNew(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	manager := NewManager(client, "https://example.com")

	token, err := manager.EnsureWebhookSecret(context.Background(), "default", "webhook-token", nil)
	require.NoError(t, err)
	assert.Len(t, token, TokenLength*2)

	// Verify secret was created
	secret := &corev1.Secret{}
	err = client.Get(context.Background(),
		types.NamespacedName{Name: "webhook-token", Namespace: "default"},
		secret)
	require.NoError(t, err)
	assert.Equal(t, token, string(secret.Data["token"]))
	assert.Equal(t, ControllerName, secret.Labels[ManagedByLabel])
}

func TestEnsureWebhookSecret_ReuseExisting(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create existing secret with token
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-token",
			Namespace: "default",
			Labels: map[string]string{
				ManagedByLabel: ControllerName,
			},
		},
		Data: map[string][]byte{
			"token": []byte("existing-token-12345678901234567890123456789012"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingSecret).
		Build()
	manager := NewManager(client, "https://example.com")

	token, err := manager.EnsureWebhookSecret(context.Background(), "default", "webhook-token", nil)
	require.NoError(t, err)
	assert.Equal(t, "existing-token-12345678901234567890123456789012", token)
}

func TestEnsureWebhookSecret_UnmanagedSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create existing secret without our label
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte("unmanaged-token"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingSecret).
		Build()
	manager := NewManager(client, "https://example.com")

	_, err := manager.EnsureWebhookSecret(context.Background(), "default", "webhook-token", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not managed by controller")
}

func TestConstructWebhookURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		webhookPath string
		expected    string
		expectError bool
	}{
		{
			name:        "simple path",
			baseURL:     "https://flux.example.com",
			webhookPath: "/hook/abc123",
			expected:    "https://flux.example.com/hook/abc123",
			expectError: false,
		},
		{
			name:        "base URL with trailing slash",
			baseURL:     "https://flux.example.com/",
			webhookPath: "/hook/abc123",
			expected:    "https://flux.example.com/hook/abc123",
			expectError: false,
		},
		{
			name:        "path without leading slash",
			baseURL:     "https://flux.example.com",
			webhookPath: "hook/abc123",
			expected:    "https://flux.example.com/hook/abc123",
			expectError: false,
		},
		{
			name:        "base URL with path",
			baseURL:     "https://flux.example.com/webhooks",
			webhookPath: "/hook/abc123",
			expected:    "https://flux.example.com/webhooks/hook/abc123",
			expectError: false,
		},
		{
			name:        "empty base URL",
			baseURL:     "",
			webhookPath: "/hook/abc123",
			expectError: true,
		},
		{
			name:        "invalid base URL",
			baseURL:     "not a valid url",
			webhookPath: "/hook/abc123",
			expectError: true,
		},
		{
			name:        "non-http scheme",
			baseURL:     "ftp://flux.example.com",
			webhookPath: "/hook/abc123",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			manager := NewManager(client, tt.baseURL)

			url, err := manager.ConstructWebhookURL(tt.webhookPath)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, url)
			}
		})
	}
}

func TestIsWebhookConfigured(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	t.Run("configured", func(t *testing.T) {
		manager := NewManager(client, "https://example.com")
		assert.True(t, manager.IsWebhookConfigured())
	})

	t.Run("not configured", func(t *testing.T) {
		manager := NewManager(client, "")
		assert.False(t, manager.IsWebhookConfigured())
	})
}
