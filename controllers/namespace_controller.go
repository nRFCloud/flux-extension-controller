package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NamespaceReconciler reconciles Namespace objects for ConfigMap syncing
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logr.Logger
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.logger.WithValues("namespace", req.Name)

	// Skip flux-system namespace
	if req.Name == FluxSystemNamespace {
		return ctrl.Result{}, nil
	}

	// Fetch the Namespace
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			// Namespace was deleted, cleanup is handled by Kubernetes garbage collection
			logger.V(1).Info("Namespace was deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to fetch Namespace")
		return ctrl.Result{}, err
	}

	// Check if this namespace should receive synced ConfigMaps
	if !r.shouldReceiveSync(namespace) {
		logger.V(1).Info("Namespace does not have sync target annotation, cleaning up any synced ConfigMaps")
		return r.cleanupSyncedConfigMapsInNamespace(ctx, namespace.Name, logger)
	}

	// Get all ConfigMaps in flux-system that should be synced
	syncableConfigMaps, err := r.getSyncableConfigMaps(ctx)
	if err != nil {
		logger.Error(err, "Failed to get syncable ConfigMaps")
		return ctrl.Result{}, err
	}

	// Sync applicable ConfigMaps to this namespace
	syncedCount := 0
	for _, configMap := range syncableConfigMaps {
		if r.shouldSyncToNamespace(namespace, &configMap) {
			if err := r.syncConfigMapToNamespace(ctx, &configMap, namespace.Name, logger); err != nil {
				logger.Error(err, "Failed to sync ConfigMap", "configMap", configMap.Name)
				return ctrl.Result{}, err
			}
			syncedCount++
		}
	}

	logger.Info("Successfully processed namespace", "syncedConfigMaps", syncedCount)
	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) shouldReceiveSync(namespace *corev1.Namespace) bool {
	if namespace.Annotations == nil {
		return false
	}
	value, exists := namespace.Annotations[SyncTargetAnnotation]
	return exists && value == "true"
}

func (r *NamespaceReconciler) getSyncableConfigMaps(ctx context.Context) ([]corev1.ConfigMap, error) {
	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList, client.InNamespace(FluxSystemNamespace)); err != nil {
		return nil, fmt.Errorf("failed to list ConfigMaps in %s: %w", FluxSystemNamespace, err)
	}

	var syncableConfigMaps []corev1.ConfigMap
	for _, cm := range configMapList.Items {
		if r.shouldSyncConfigMap(&cm) {
			syncableConfigMaps = append(syncableConfigMaps, cm)
		}
	}

	return syncableConfigMaps, nil
}

func (r *NamespaceReconciler) shouldSyncConfigMap(configMap *corev1.ConfigMap) bool {
	if configMap.Annotations == nil {
		return false
	}
	value, exists := configMap.Annotations[SyncConfigMapAnnotation]
	return exists && value == "true"
}

func (r *NamespaceReconciler) shouldSyncToNamespace(namespace *corev1.Namespace, configMap *corev1.ConfigMap) bool {
	// Check if ConfigMap has specific namespace targets first
	if configMap.Annotations != nil {
		if namespaces, exists := configMap.Annotations[SyncConfigMapAnnotation+"/namespaces"]; exists {
			targetNamespaces := splitAndTrim(namespaces, ",")
			for _, target := range targetNamespaces {
				if target == namespace.Name {
					return true
				}
			}
			return false
		}
	}

	// If no specific ConfigMap targets, check namespace annotations
	if namespace.Annotations == nil {
		return false
	}

	// Check if namespace has sync target annotation
	syncValue, exists := namespace.Annotations[SyncTargetAnnotation]
	if !exists || syncValue != "true" {
		return false
	}

	// Check if namespace has specific ConfigMap filters
	if filter, exists := namespace.Annotations[SyncTargetAnnotation+"/configmaps"]; exists {
		allowedConfigMaps := splitAndTrim(filter, ",")
		for _, allowed := range allowedConfigMaps {
			if allowed == configMap.Name {
				return true
			}
		}
		return false
	}

	// If no specific filters, sync by default
	return true
}

func (r *NamespaceReconciler) syncConfigMapToNamespace(ctx context.Context, sourceConfigMap *corev1.ConfigMap, targetNamespace string, logger logr.Logger) error {
	// This is similar to the ConfigMapReconciler method, but we'll reuse the logic
	configMapReconciler := &ConfigMapReconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}
	return configMapReconciler.syncConfigMapToNamespace(ctx, sourceConfigMap, targetNamespace, logger)
}

func (r *NamespaceReconciler) cleanupSyncedConfigMapsInNamespace(ctx context.Context, namespaceName string, logger logr.Logger) (ctrl.Result, error) {
	// Find all synced ConfigMaps in this namespace
	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList, client.InNamespace(namespaceName)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list ConfigMaps in namespace %s: %w", namespaceName, err)
	}

	for _, cm := range configMapList.Items {
		if cm.Annotations != nil && cm.Annotations[SyncSourceAnnotation] != "" {
			if err := r.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to delete synced ConfigMap", "configMap", cm.Name)
				return ctrl.Result{}, err
			}
			logger.Info("Deleted synced ConfigMap", "configMap", cm.Name)
		}
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.logger = ctrl.Log.WithName("namespace-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			// Skip flux-system namespace
			return object.GetName() != FluxSystemNamespace
		})).
		Complete(r)
}

// Helper function to split and trim strings
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0)
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if s == "" {
		return nil
	}
	// Simple split implementation
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimString(s string) string {
	start := 0
	end := len(s)

	// Trim leading spaces
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// Trim trailing spaces
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
