package controllers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	notificationv1 "github.com/fluxcd/notification-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/nrfcloud/flux-extension-controller/pkg/config"
	"github.com/nrfcloud/flux-extension-controller/pkg/github"
	"github.com/nrfcloud/flux-extension-controller/pkg/webhook"
)

const (
	// ReceiverFinalizer is the finalizer for Receiver resources
	ReceiverFinalizer = "flux-extension-controller.nrfcloud.com/receiver"
	// WebhookIDAnnotation stores the GitHub webhook ID
	WebhookIDAnnotation = "flux-extension-controller.nrfcloud.com/webhook-id"
	// RepositoryURLAnnotation stores the repository URL
	RepositoryURLAnnotation = "flux-extension-controller.nrfcloud.com/repository-url"
)

// ReceiverReconciler reconciles Receiver objects
type ReceiverReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *config.Config

	webhookManager *webhook.Manager
	githubClient   *github.Client
	logger         logr.Logger
}

// +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

// Reconcile implements the reconciliation logic for Receiver resources
func (r *ReceiverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.logger.WithValues("receiver", req.NamespacedName)

	// Skip if webhook feature is not configured
	if !r.webhookManager.IsWebhookConfigured() {
		logger.V(1).Info("Webhook feature not configured, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Fetch the Receiver instance
	receiver := &notificationv1.Receiver{}
	if err := r.Get(ctx, req.NamespacedName, receiver); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Receiver")
		return ctrl.Result{}, err
	}

	// Check if receiver is suspended
	if receiver.Spec.Suspend {
		logger.V(1).Info("Receiver is suspended, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Only handle GitHub receivers
	if receiver.Spec.Type != "github" {
		logger.V(1).Info("Receiver is not of type github, skipping", "type", receiver.Spec.Type)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !receiver.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, receiver, logger)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(receiver, ReceiverFinalizer) {
		controllerutil.AddFinalizer(receiver, ReceiverFinalizer)
		if err := r.Update(ctx, receiver); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced GitRepository
	_, repoURL, err := r.getReferencedGitRepository(ctx, receiver)
	if err != nil {
		logger.Error(err, "Failed to get referenced GitRepository")
		r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "GitRepositoryNotFound", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Validate repository belongs to configured organization
	if err := r.githubClient.ValidateRepositoryURL(repoURL); err != nil {
		logger.Error(err, "Repository URL validation failed")
		r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "ValidationFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Ensure webhook secret exists or reuse existing
	token, err := r.webhookManager.EnsureWebhookSecret(ctx, receiver.Namespace, receiver.Spec.SecretRef.Name, receiver)
	if err != nil {
		logger.Error(err, "Failed to ensure webhook secret")
		r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "SecretFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Wait for webhookPath to be populated by notification controller
	if receiver.Status.WebhookPath == "" {
		logger.Info("Waiting for webhook path to be populated by notification controller")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Construct full webhook URL
	webhookURL, err := r.webhookManager.ConstructWebhookURL(receiver.Status.WebhookPath)
	if err != nil {
		logger.Error(err, "Failed to construct webhook URL")
		r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "WebhookURLFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Get events to configure
	events := receiver.Spec.Events
	if len(events) == 0 {
		// Default events for GitHub
		events = []string{"push"}
	}

	// Check if webhook already exists
	webhookManager := github.NewWebhookManager(r.githubClient)
	if webhookIDStr, exists := receiver.Annotations[WebhookIDAnnotation]; exists {
		webhookID, err := strconv.ParseInt(webhookIDStr, 10, 64)
		if err == nil {
			// Try to get existing webhook
			existingHook, err := webhookManager.GetWebhook(ctx, repoURL, webhookID)
			if err == nil {
				// Update existing webhook
				logger.Info("Updating existing webhook", "webhookID", webhookID)
				_, err = webhookManager.UpdateWebhook(ctx, repoURL, webhookID, webhookURL, token, events)
				if err != nil {
					logger.Error(err, "Failed to update webhook")
					r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "WebhookUpdateFailed", err.Error())
					return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
				}

				// Update status
				r.updateReceiverStatus(ctx, receiver, metav1.ConditionTrue, "WebhookUpdated",
					fmt.Sprintf("GitHub webhook updated: %s (ID: %d)", webhookURL, existingHook.GetID()))
				return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
			}
			// Webhook doesn't exist anymore, will create new one
			logger.Info("Existing webhook not found, creating new one")
		}
	}

	// Create new webhook
	logger.Info("Creating new webhook", "url", webhookURL, "events", events)
	hook, err := webhookManager.CreateWebhook(ctx, repoURL, webhookURL, token, events)
	if err != nil {
		logger.Error(err, "Failed to create webhook")
		r.updateReceiverStatus(ctx, receiver, metav1.ConditionFalse, "WebhookCreateFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Store webhook ID in annotations
	if receiver.Annotations == nil {
		receiver.Annotations = make(map[string]string)
	}
	receiver.Annotations[WebhookIDAnnotation] = strconv.FormatInt(hook.GetID(), 10)
	receiver.Annotations[RepositoryURLAnnotation] = repoURL
	if err := r.Update(ctx, receiver); err != nil {
		logger.Error(err, "Failed to update receiver annotations")
		return ctrl.Result{}, err
	}

	// Update status
	r.updateReceiverStatus(ctx, receiver, metav1.ConditionTrue, "WebhookCreated",
		fmt.Sprintf("GitHub webhook created: %s (ID: %d)", webhookURL, hook.GetID()))

	logger.Info("Successfully reconciled Receiver")
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

// reconcileDelete handles receiver deletion
func (r *ReceiverReconciler) reconcileDelete(ctx context.Context, receiver *notificationv1.Receiver, logger logr.Logger) (ctrl.Result, error) {
	logger.Info("Deleting receiver")

	// Delete webhook if it exists
	if webhookIDStr, exists := receiver.Annotations[WebhookIDAnnotation]; exists {
		webhookID, err := strconv.ParseInt(webhookIDStr, 10, 64)
		if err == nil {
			repoURL := receiver.Annotations[RepositoryURLAnnotation]
			if repoURL != "" {
				webhookManager := github.NewWebhookManager(r.githubClient)
				logger.Info("Deleting webhook", "webhookID", webhookID)
				if err := webhookManager.DeleteWebhook(ctx, repoURL, webhookID); err != nil {
					logger.Error(err, "Failed to delete webhook, continuing with finalizer removal")
				} else {
					logger.Info("Successfully deleted webhook")
				}
			}
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(receiver, ReceiverFinalizer)
	if err := r.Update(ctx, receiver); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// getReferencedGitRepository finds the GitRepository referenced by the receiver
func (r *ReceiverReconciler) getReferencedGitRepository(ctx context.Context, receiver *notificationv1.Receiver) (*sourcev1.GitRepository, string, error) {
	// Look for GitRepository in resources
	for _, resource := range receiver.Spec.Resources {
		if resource.Kind == "GitRepository" && (resource.APIVersion == "" || resource.APIVersion == sourcev1.GroupVersion.String()) {
			gitRepo := &sourcev1.GitRepository{}
			namespace := receiver.Namespace
			if resource.Namespace != "" {
				namespace = resource.Namespace
			}

			if err := r.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      resource.Name,
			}, gitRepo); err != nil {
				return nil, "", fmt.Errorf("failed to get GitRepository %s/%s: %w", namespace, resource.Name, err)
			}

			return gitRepo, gitRepo.Spec.URL, nil
		}
	}

	return nil, "", fmt.Errorf("no GitRepository found in receiver resources")
}

// updateReceiverStatus updates the Receiver status
func (r *ReceiverReconciler) updateReceiverStatus(ctx context.Context, receiver *notificationv1.Receiver,
	status metav1.ConditionStatus, reason, message string) {

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: receiver.Generation,
		LastTransitionTime: metav1.Now(),
	}

	meta.SetStatusCondition(&receiver.Status.Conditions, condition)

	if err := r.Status().Update(ctx, receiver); err != nil {
		r.logger.Error(err, "Failed to update Receiver status")
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *ReceiverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize logger
	r.logger = ctrl.Log.WithName("controllers").WithName("Receiver")

	// Initialize GitHub client
	githubClient, err := github.NewClient(&r.Config.GitHub)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	r.githubClient = githubClient

	// Initialize webhook manager
	r.webhookManager = webhook.NewManager(r.Client, r.Config.Webhook.BaseURL)

	// Skip setting up controller if webhook is not configured
	if !r.webhookManager.IsWebhookConfigured() {
		r.logger.Info("Webhook feature not configured, skipping Receiver controller setup")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&notificationv1.Receiver{}).
		Complete(r)
}
