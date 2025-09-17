package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/nrfcloud/flux-extension-controller/pkg/config"
	"github.com/nrfcloud/flux-extension-controller/pkg/github"
	"github.com/nrfcloud/flux-extension-controller/pkg/kubernetes"
	"github.com/nrfcloud/flux-extension-controller/pkg/token"
)

// GitRepositoryReconciler reconciles GitRepository objects
type GitRepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *config.Config

	githubClient   github.GitHubClient
	secretManager  *kubernetes.SecretManager
	refreshManager token.RefreshManagerInterface
	logger         logr.Logger
}

// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the reconciliation logic for GitRepository resources
func (r *GitRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.logger.WithValues("gitrepository", req.NamespacedName)

	// Fetch the GitRepository instance
	gitRepo := &sourcev1.GitRepository{}
	if err := r.Get(ctx, req.NamespacedName, gitRepo); err != nil {
		if apierrors.IsNotFound(err) {
			// GitRepository was deleted, clean up any scheduled refreshes
			r.refreshManager.CancelRefresh(req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get GitRepository")
		return ctrl.Result{}, err
	}

	// Check if namespace is excluded
	if r.isNamespaceExcluded(gitRepo.Namespace) {
		logger.V(1).Info("Skipping GitRepository in excluded namespace")
		return ctrl.Result{}, nil
	}

	// Check if this is a repository from the target organization
	if !r.isTargetOrganizationRepository(gitRepo.Spec.URL) {
		logger.V(1).Info("Skipping repository from different organization", "url", gitRepo.Spec.URL)
		return ctrl.Result{}, nil
	}

	// Validate repository URL
	if err := r.githubClient.ValidateRepositoryURL(gitRepo.Spec.URL); err != nil {
		logger.Error(err, "Repository URL validation failed")
		r.updateGitRepositoryStatus(ctx, gitRepo, metav1.ConditionFalse, "ValidationFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Check if secretRef is specified
	if gitRepo.Spec.SecretRef == nil {
		logger.V(1).Info("No secretRef specified, skipping")
		return ctrl.Result{}, nil
	}

	secretName := gitRepo.Spec.SecretRef.Name
	secretNamespace := gitRepo.Namespace

	// Validate secret ownership
	if err := r.secretManager.ValidateSecretOwnership(ctx, secretNamespace, secretName, gitRepo.Spec.URL); err != nil {
		logger.Error(err, "Secret ownership validation failed")
		r.updateGitRepositoryStatus(ctx, gitRepo, metav1.ConditionFalse, "SecretValidationFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Generate GitHub installation token
	installationToken, err := r.githubClient.GenerateInstallationToken(ctx, gitRepo.Spec.URL)
	if err != nil {
		logger.Error(err, "Failed to generate installation token")
		r.updateGitRepositoryStatus(ctx, gitRepo, metav1.ConditionFalse, "TokenGenerationFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Create or update the secret
	if err := r.secretManager.CreateOrUpdateSecret(
		ctx,
		secretNamespace,
		secretName,
		installationToken,
		gitRepo.Spec.URL,
		gitRepo,
	); err != nil {
		logger.Error(err, "Failed to create or update secret")
		r.updateGitRepositoryStatus(ctx, gitRepo, metav1.ConditionFalse, "SecretUpdateFailed", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Schedule token refresh
	if err := r.refreshManager.ScheduleRefresh(ctx, secretNamespace, secretName, gitRepo.Spec.URL); err != nil {
		logger.Error(err, "Failed to schedule token refresh")
		// Don't fail the reconciliation for refresh scheduling errors
	}

	// Update GitRepository status
	r.updateGitRepositoryStatus(ctx, gitRepo, metav1.ConditionTrue, "TokenCreated",
		fmt.Sprintf("GitHub token created and scheduled for refresh at %s", installationToken.GetExpiresAt().Format(time.RFC3339)))

	logger.Info("Successfully reconciled GitRepository")
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

// isNamespaceExcluded checks if the namespace should be excluded from processing using glob patterns
func (r *GitRepositoryReconciler) isNamespaceExcluded(namespace string) bool {
	for _, excluded := range r.Config.Controller.ExcludedNamespaces {
		// Use filepath.Match for glob pattern matching
		matched, err := filepath.Match(excluded, namespace)
		if err != nil {
			// If pattern is invalid, fall back to exact string matching
			r.logger.V(1).Info("Invalid glob pattern, using exact match", "pattern", excluded, "error", err)
			if namespace == excluded {
				return true
			}
		} else if matched {
			return true
		}
	}
	return false
}

// isTargetOrganizationRepository checks if the repository URL belongs to the configured organization
func (r *GitRepositoryReconciler) isTargetOrganizationRepository(url string) bool {
	orgPrefix := fmt.Sprintf("https://github.com/%s/", r.Config.GitHub.Organization)
	return strings.HasPrefix(url, orgPrefix)
}

// updateGitRepositoryStatus updates the GitRepository status
func (r *GitRepositoryReconciler) updateGitRepositoryStatus(ctx context.Context, gitRepo *sourcev1.GitRepository,
	status metav1.ConditionStatus, reason, message string) {

	// Find existing condition or create new one
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	// Update the condition
	meta.SetStatusCondition(&gitRepo.Status.Conditions, condition)

	// Update the status
	if err := r.Status().Update(ctx, gitRepo); err != nil {
		r.logger.Error(err, "Failed to update GitRepository status")
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *GitRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize logger
	r.logger = ctrl.Log.WithName("controllers").WithName("GitRepository")

	// Initialize GitHub client
	githubClient, err := github.NewClient(&r.Config.GitHub)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	r.githubClient = githubClient

	// Initialize secret manager
	r.secretManager = kubernetes.NewSecretManager(r.Client)

	// Initialize refresh manager
	r.refreshManager = token.NewRefreshManager(
		r.Client,
		r.githubClient,
		r.secretManager,
		r.Config.TokenRefresh.RefreshInterval,
		r.logger,
	)

	// Start refresh manager
	ctx := context.Background()
	if err := r.refreshManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start refresh manager: %w", err)
	}

	// Create predicate to filter events
	namespacePredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return !r.isNamespaceExcluded(object.GetNamespace())
	})

	// Build and return the controller
	return ctrl.NewControllerManagedBy(mgr).
		For(&sourcev1.GitRepository{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		WithEventFilter(namespacePredicate).
		Complete(r)
}
