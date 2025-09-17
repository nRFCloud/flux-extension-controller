package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// Annotation on ConfigMaps in flux-system to indicate they should be synced
	SyncConfigMapAnnotation = "flux-extension.nrfcloud.com/sync-configmap"
	// Annotation on Namespaces to indicate they want to receive synced ConfigMaps
	SyncTargetAnnotation = "flux-extension.nrfcloud.com/sync-target"
	// Annotation to track the source of synced ConfigMaps
	SyncSourceAnnotation = "flux-extension.nrfcloud.com/sync-source"
	// The source namespace for ConfigMaps
	FluxSystemNamespace = "flux-system"
)

// ConfigMapReconciler reconciles ConfigMap objects in flux-system namespace
type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logr.Logger
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.logger.WithValues("configmap", req.NamespacedName)

	// Only process ConfigMaps in flux-system namespace
	if req.Namespace != FluxSystemNamespace {
		return ctrl.Result{}, nil
	}

	// Fetch the ConfigMap
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			// ConfigMap was deleted, clean up synced copies
			return r.cleanupSyncedConfigMaps(ctx, req.Name, logger)
		}
		logger.Error(err, "Failed to fetch ConfigMap")
		return ctrl.Result{}, err
	}

	// Check if this ConfigMap should be synced
	if !r.shouldSyncConfigMap(configMap) {
		logger.V(1).Info("ConfigMap does not have sync annotation, skipping")
		return ctrl.Result{}, nil
	}

	// Get all target namespaces
	targetNamespaces, err := r.getTargetNamespaces(ctx, configMap)
	if err != nil {
		logger.Error(err, "Failed to get target namespaces")
		return ctrl.Result{}, err
	}

	// Sync ConfigMap to target namespaces
	for _, namespace := range targetNamespaces {
		if err := r.syncConfigMapToNamespace(ctx, configMap, namespace, logger); err != nil {
			logger.Error(err, "Failed to sync ConfigMap to namespace", "targetNamespace", namespace)
			return ctrl.Result{}, err
		}
	}

	logger.Info("Successfully synced ConfigMap", "targetNamespaces", len(targetNamespaces))
	return ctrl.Result{}, nil
}

func (r *ConfigMapReconciler) shouldSyncConfigMap(configMap *corev1.ConfigMap) bool {
	if configMap.Annotations == nil {
		return false
	}
	value, exists := configMap.Annotations[SyncConfigMapAnnotation]
	return exists && strings.ToLower(value) == "true"
}

func (r *ConfigMapReconciler) getTargetNamespaces(ctx context.Context, configMap *corev1.ConfigMap) ([]string, error) {
	var targetNamespaces []string

	// Check if specific namespaces are specified in the annotation
	if configMap.Annotations != nil {
		if namespaces, exists := configMap.Annotations["flux-extension.nrfcloud.com/sync-configmap-namespaces"]; exists {
			return strings.Split(namespaces, ","), nil
		}
	}

	// Get all namespaces with sync target annotation
	namespaceList := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaceList); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	for _, ns := range namespaceList.Items {
		if r.shouldReceiveSync(&ns, configMap) {
			targetNamespaces = append(targetNamespaces, ns.Name)
		}
	}

	return targetNamespaces, nil
}

func (r *ConfigMapReconciler) shouldReceiveSync(namespace *corev1.Namespace, configMap *corev1.ConfigMap) bool {
	if namespace.Annotations == nil {
		return false
	}

	// Skip flux-system namespace
	if namespace.Name == FluxSystemNamespace {
		return false
	}

	syncValue, exists := namespace.Annotations[SyncTargetAnnotation]
	if !exists || strings.ToLower(syncValue) != "true" {
		return false
	}

	// Check if namespace has specific ConfigMap filters
	if filter, exists := namespace.Annotations["flux-extension.nrfcloud.com/sync-target-configmaps"]; exists {
		allowedConfigMaps := strings.Split(filter, ",")
		for _, allowed := range allowedConfigMaps {
			if strings.TrimSpace(allowed) == configMap.Name {
				return true
			}
		}
		return false
	}

	return true
}

func (r *ConfigMapReconciler) syncConfigMapToNamespace(ctx context.Context, sourceConfigMap *corev1.ConfigMap, targetNamespace string, logger logr.Logger) error {
	targetConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sourceConfigMap.Name,
			Namespace: targetNamespace,
			Annotations: map[string]string{
				SyncSourceAnnotation: fmt.Sprintf("%s/%s", FluxSystemNamespace, sourceConfigMap.Name),
			},
		},
		Data:       make(map[string]string),
		BinaryData: make(map[string][]byte),
	}

	// Copy data from source
	for key, value := range sourceConfigMap.Data {
		targetConfigMap.Data[key] = value
	}
	for key, value := range sourceConfigMap.BinaryData {
		targetConfigMap.BinaryData[key] = value
	}

	// Copy relevant annotations (excluding sync annotations)
	if sourceConfigMap.Annotations != nil {
		if targetConfigMap.Annotations == nil {
			targetConfigMap.Annotations = make(map[string]string)
		}
		for key, value := range sourceConfigMap.Annotations {
			if !strings.HasPrefix(key, "flux-extension.nrfcloud.com/sync") {
				targetConfigMap.Annotations[key] = value
			}
		}
		// Ensure we keep the source annotation
		targetConfigMap.Annotations[SyncSourceAnnotation] = fmt.Sprintf("%s/%s", FluxSystemNamespace, sourceConfigMap.Name)
	}

	// Check if ConfigMap already exists
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: targetConfigMap.Name, Namespace: targetNamespace}, existingConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new ConfigMap
			if err := r.Create(ctx, targetConfigMap); err != nil {
				return fmt.Errorf("failed to create ConfigMap in namespace %s: %w", targetNamespace, err)
			}
			logger.Info("Created synced ConfigMap", "targetNamespace", targetNamespace)
		} else {
			return fmt.Errorf("failed to check existing ConfigMap: %w", err)
		}
	} else {
		// Update existing ConfigMap only if it's a synced one
		if existingConfigMap.Annotations[SyncSourceAnnotation] == fmt.Sprintf("%s/%s", FluxSystemNamespace, sourceConfigMap.Name) {
			existingConfigMap.Data = targetConfigMap.Data
			existingConfigMap.BinaryData = targetConfigMap.BinaryData
			existingConfigMap.Annotations = targetConfigMap.Annotations
			if err := r.Update(ctx, existingConfigMap); err != nil {
				return fmt.Errorf("failed to update ConfigMap in namespace %s: %w", targetNamespace, err)
			}
			logger.Info("Updated synced ConfigMap", "targetNamespace", targetNamespace)
		}
	}

	return nil
}

func (r *ConfigMapReconciler) cleanupSyncedConfigMaps(ctx context.Context, configMapName string, logger logr.Logger) (ctrl.Result, error) {
	// Find all synced ConfigMaps across namespaces
	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	sourceReference := fmt.Sprintf("%s/%s", FluxSystemNamespace, configMapName)
	for _, cm := range configMapList.Items {
		if cm.Annotations != nil && cm.Annotations[SyncSourceAnnotation] == sourceReference {
			if err := r.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to delete synced ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
				return ctrl.Result{}, err
			}
			logger.Info("Deleted synced ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
		}
	}

	return ctrl.Result{}, nil
}

func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.logger = ctrl.Log.WithName("configmap-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			// Only watch ConfigMaps in flux-system namespace
			return object.GetNamespace() == FluxSystemNamespace
		})).
		Complete(r)
}
