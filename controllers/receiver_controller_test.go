package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	notificationv1 "github.com/fluxcd/notification-controller/api/v1"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/nrfcloud/flux-extension-controller/pkg/config"
	"github.com/nrfcloud/flux-extension-controller/pkg/webhook"
)

func TestReceiverReconciler_SkipWhenNotConfigured(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = sourcev1.AddToScheme(s)
	_ = notificationv1.AddToScheme(s)

	receiver := &notificationv1.Receiver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-receiver",
			Namespace: "default",
		},
		Spec: notificationv1.ReceiverSpec{
			Type: "github",
			SecretRef: fluxmeta.LocalObjectReference{
				Name: "webhook-token",
			},
			Resources: []notificationv1.CrossNamespaceObjectReference{
				{
					Kind:       "GitRepository",
					Name:       "test-repo",
					APIVersion: sourcev1.GroupVersion.String(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(receiver).
		Build()

	cfg := &config.Config{
		Webhook: config.WebhookConfig{
			BaseURL: "", // Not configured
		},
		GitHub: config.GitHubConfig{
			Organization: "test-org",
		},
	}

	reconciler := &ReceiverReconciler{
		Client: client,
		Scheme: s,
		Config: cfg,
		logger: logr.Discard(),
	}

	// Initialize webhook manager
	reconciler.webhookManager = webhook.NewManager(client, cfg.Webhook.BaseURL)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-receiver",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReceiverReconciler_SkipNonGitHubReceivers(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = sourcev1.AddToScheme(s)
	_ = notificationv1.AddToScheme(s)

	receiver := &notificationv1.Receiver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-receiver",
			Namespace: "default",
		},
		Spec: notificationv1.ReceiverSpec{
			Type: "gitlab", // Not github
			SecretRef: fluxmeta.LocalObjectReference{
				Name: "webhook-token",
			},
			Resources: []notificationv1.CrossNamespaceObjectReference{
				{
					Kind:       "GitRepository",
					Name:       "test-repo",
					APIVersion: sourcev1.GroupVersion.String(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(receiver).
		Build()

	cfg := &config.Config{
		Webhook: config.WebhookConfig{
			BaseURL: "https://example.com",
		},
		GitHub: config.GitHubConfig{
			Organization: "test-org",
		},
	}

	reconciler := &ReceiverReconciler{
		Client: client,
		Scheme: s,
		Config: cfg,
		logger: logr.Discard(),
	}

	// Initialize webhook manager
	reconciler.webhookManager = webhook.NewManager(client, cfg.Webhook.BaseURL)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-receiver",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReceiverReconciler_SkipSuspendedReceivers(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = sourcev1.AddToScheme(s)
	_ = notificationv1.AddToScheme(s)

	receiver := &notificationv1.Receiver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-receiver",
			Namespace: "default",
		},
		Spec: notificationv1.ReceiverSpec{
			Type:    "github",
			Suspend: true, // Suspended
			SecretRef: fluxmeta.LocalObjectReference{
				Name: "webhook-token",
			},
			Resources: []notificationv1.CrossNamespaceObjectReference{
				{
					Kind:       "GitRepository",
					Name:       "test-repo",
					APIVersion: sourcev1.GroupVersion.String(),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(receiver).
		Build()

	cfg := &config.Config{
		Webhook: config.WebhookConfig{
			BaseURL: "https://example.com",
		},
		GitHub: config.GitHubConfig{
			Organization: "test-org",
		},
	}

	reconciler := &ReceiverReconciler{
		Client: client,
		Scheme: s,
		Config: cfg,
		logger: logr.Discard(),
	}

	// Initialize webhook manager
	reconciler.webhookManager = webhook.NewManager(client, cfg.Webhook.BaseURL)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-receiver",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

// Note: More comprehensive tests would require mocking of GitHub API operations
// or integration tests with a real GitHub App. The tests above cover the basic
// controller logic paths (skip conditions).
