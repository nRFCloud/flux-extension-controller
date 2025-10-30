package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-github/v76/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSecretManager_CreateOrUpdateSecret(t *testing.T) {
	// Set up fake client
	s := scheme.Scheme
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	secretManager := NewSecretManager(fakeClient)

	ctx := context.Background()
	namespace := "test-namespace"
	name := "test-secret"
	repositoryURL := "https://github.com/nrfcloud/test-repo"

	// Create mock installation token
	expiresAt := time.Now().Add(1 * time.Hour)
	token := &github.InstallationToken{
		Token:     github.String("test-token-123"),
		ExpiresAt: &github.Timestamp{Time: expiresAt},
	}

	// Create mock owner object
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-owner",
			Namespace: namespace,
			UID:       "test-uid",
		},
	}

	// Test creating a new secret
	err := secretManager.CreateOrUpdateSecret(ctx, namespace, name, token, repositoryURL, owner)
	require.NoError(t, err)

	// Verify secret was created
	secret := &corev1.Secret{}
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	require.NoError(t, err)

	// Verify secret content
	assert.Equal(t, SecretTypeGitRepository, string(secret.Type))
	assert.Equal(t, []byte("git"), secret.Data["username"])
	assert.Equal(t, []byte("test-token-123"), secret.Data["password"])

	// Verify annotations
	assert.Equal(t, "flux-extension-controller", secret.Annotations[AnnotationManagedBy])
	assert.Equal(t, expiresAt.Format(time.RFC3339), secret.Annotations[AnnotationTokenExpiry])
	assert.Equal(t, repositoryURL, secret.Annotations[AnnotationRepositoryURL])

	// Verify owner reference
	assert.Len(t, secret.OwnerReferences, 1)
	assert.Equal(t, owner.UID, secret.OwnerReferences[0].UID)

	// Test updating existing secret
	newExpiresAt := time.Now().Add(2 * time.Hour)
	newToken := &github.InstallationToken{
		Token:     github.String("new-token-456"),
		ExpiresAt: &github.Timestamp{Time: newExpiresAt},
	}

	err = secretManager.CreateOrUpdateSecret(ctx, namespace, name, newToken, repositoryURL, owner)
	require.NoError(t, err)

	// Verify secret was updated
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	require.NoError(t, err)
	assert.Equal(t, []byte("new-token-456"), secret.Data["password"])
	assert.Equal(t, newExpiresAt.Format(time.RFC3339), secret.Annotations[AnnotationTokenExpiry])
}

func TestSecretManager_GetSecret(t *testing.T) {
	// Set up fake client with existing secret
	s := scheme.Scheme
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"username": []byte("git"),
			"password": []byte("test-token"),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existingSecret).Build()
	secretManager := NewSecretManager(fakeClient)

	ctx := context.Background()

	// Test getting existing secret
	secret, err := secretManager.GetSecret(ctx, "test-namespace", "existing-secret")
	require.NoError(t, err)
	assert.Equal(t, "existing-secret", secret.Name)
	assert.Equal(t, "test-namespace", secret.Namespace)

	// Test getting non-existent secret
	_, err = secretManager.GetSecret(ctx, "test-namespace", "non-existent")
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
}

func TestSecretManager_IsSecretManagedByController(t *testing.T) {
	secretManager := NewSecretManager(nil)

	tests := []struct {
		name     string
		secret   *corev1.Secret
		expected bool
	}{
		{
			name: "managed secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy: "flux-extension-controller",
					},
				},
			},
			expected: true,
		},
		{
			name: "not managed secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy: "other-controller",
					},
				},
			},
			expected: false,
		},
		{
			name: "no annotations",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: false,
		},
		{
			name: "no managed-by annotation",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"other-annotation": "value",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := secretManager.IsSecretManagedByController(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSecretManager_GetTokenExpiry(t *testing.T) {
	secretManager := NewSecretManager(nil)

	expiryTime := time.Now().Add(1 * time.Hour)
	expiryString := expiryTime.Format(time.RFC3339)

	tests := []struct {
		name         string
		secret       *corev1.Secret
		expectedTime time.Time
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid expiry annotation",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationTokenExpiry: expiryString,
					},
				},
			},
			expectedTime: expiryTime,
			expectError:  false,
		},
		{
			name: "no annotations",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expectError: true,
			errorMsg:    "secret has no annotations",
		},
		{
			name: "missing expiry annotation",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"other-annotation": "value",
					},
				},
			},
			expectError: true,
			errorMsg:    "secret has no token expiry annotation",
		},
		{
			name: "invalid expiry format",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationTokenExpiry: "invalid-time",
					},
				},
			},
			expectError: true,
			errorMsg:    "failed to parse token expiry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := secretManager.GetTokenExpiry(tt.secret)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				// Allow for small time differences due to parsing
				assert.WithinDuration(t, tt.expectedTime, result, time.Second)
			}
		})
	}
}

func TestSecretManager_NeedsTokenRefresh(t *testing.T) {
	secretManager := NewSecretManager(nil)

	refreshThreshold := 10 * time.Minute

	tests := []struct {
		name          string
		secret        *corev1.Secret
		expectedNeeds bool
		expectError   bool
	}{
		{
			name: "token expires soon",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy:   "flux-extension-controller",
						AnnotationTokenExpiry: time.Now().Add(5 * time.Minute).Format(time.RFC3339),
					},
				},
			},
			expectedNeeds: true,
			expectError:   false,
		},
		{
			name: "token has plenty of time",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy:   "flux-extension-controller",
						AnnotationTokenExpiry: time.Now().Add(30 * time.Minute).Format(time.RFC3339),
					},
				},
			},
			expectedNeeds: false,
			expectError:   false,
		},
		{
			name: "not managed by controller",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy: "other-controller",
					},
				},
			},
			expectedNeeds: false,
			expectError:   false,
		},
		{
			name: "invalid expiry annotation",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationManagedBy:   "flux-extension-controller",
						AnnotationTokenExpiry: "invalid-time",
					},
				},
			},
			expectedNeeds: true, // Should need refresh if we can't determine expiry
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needs, err := secretManager.NeedsTokenRefresh(tt.secret, refreshThreshold)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedNeeds, needs)
		})
	}
}

func TestSecretManager_ValidateSecretOwnership(t *testing.T) {
	s := scheme.Scheme
	repositoryURL := "https://github.com/nrfcloud/test-repo"

	tests := []struct {
		name           string
		existingSecret *corev1.Secret
		expectError    bool
		errorMsg       string
	}{
		{
			name:           "secret doesn't exist",
			existingSecret: nil,
			expectError:    false,
		},
		{
			name: "managed by controller for same repo",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						AnnotationManagedBy:     "flux-extension-controller",
						AnnotationRepositoryURL: repositoryURL,
					},
				},
			},
			expectError: false,
		},
		{
			name: "not managed by controller",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						AnnotationManagedBy: "other-controller",
					},
				},
			},
			expectError: true,
			errorMsg:    "exists but is not managed by flux-extension-controller",
		},
		{
			name: "managed by controller for different repo",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						AnnotationManagedBy:     "flux-extension-controller",
						AnnotationRepositoryURL: "https://github.com/nrfcloud/other-repo",
					},
				},
			},
			expectError: true,
			errorMsg:    "managed by controller but for different repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fakeClient client.Client
			if tt.existingSecret != nil {
				fakeClient = fake.NewClientBuilder().WithScheme(s).WithObjects(tt.existingSecret).Build()
			} else {
				fakeClient = fake.NewClientBuilder().WithScheme(s).Build()
			}

			secretManager := NewSecretManager(fakeClient)
			ctx := context.Background()

			err := secretManager.ValidateSecretOwnership(ctx, "test-namespace", "test-secret", repositoryURL)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
