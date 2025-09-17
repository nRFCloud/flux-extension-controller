package controllers

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
)

// Integration test that tests the full sync workflow
func TestConfigMapSync_IntegrationWorkflow(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()

	// Initial setup - create namespaces and a ConfigMap
	targetNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "production",
			Annotations: map[string]string{
				SyncTargetAnnotation: "true",
			},
		},
	}

	sourceConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-config",
			Namespace: FluxSystemNamespace,
			Annotations: map[string]string{
				SyncConfigMapAnnotation: "true",
			},
		},
		Data: map[string]string{
			"app.properties": "database.url=postgresql://db:5432/myapp",
			"log.level":      "INFO",
		},
		BinaryData: map[string][]byte{
			"cert.pem": []byte("-----BEGIN CERTIFICATE-----\nfake cert\n-----END CERTIFICATE-----"),
		},
	}

	objects := []client.Object{targetNamespace, sourceConfigMap}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	// Create controllers
	configMapReconciler := &ConfigMapReconciler{
		Client: fakeClient,
		Scheme: scheme,
		logger: zap.New(zap.UseDevMode(true)),
	}

	namespaceReconciler := &NamespaceReconciler{
		Client: fakeClient,
		Scheme: scheme,
		logger: zap.New(zap.UseDevMode(true)),
	}

	// Test 1: ConfigMap reconciliation should sync to target namespace
	t.Run("ConfigMap sync creates synced copy", func(t *testing.T) {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceConfigMap.Namespace,
			},
		}

		result, err := configMapReconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify synced ConfigMap was created
		syncedConfigMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      sourceConfigMap.Name,
			Namespace: targetNamespace.Name,
		}, syncedConfigMap)
		require.NoError(t, err)

		// Verify data is correctly synced
		assert.Equal(t, sourceConfigMap.Data, syncedConfigMap.Data)
		assert.Equal(t, sourceConfigMap.BinaryData, syncedConfigMap.BinaryData)

		// Verify sync source annotation
		expectedSource := FluxSystemNamespace + "/" + sourceConfigMap.Name
		assert.Equal(t, expectedSource, syncedConfigMap.Annotations[SyncSourceAnnotation])
	})

	// Test 2: Update source ConfigMap and verify sync
	t.Run("ConfigMap update propagates to synced copy", func(t *testing.T) {
		// Update source ConfigMap
		err := fakeClient.Get(ctx, types.NamespacedName{
			Name:      sourceConfigMap.Name,
			Namespace: sourceConfigMap.Namespace,
		}, sourceConfigMap)
		require.NoError(t, err)

		sourceConfigMap.Data["log.level"] = "DEBUG"
		sourceConfigMap.Data["new.setting"] = "value"
		err = fakeClient.Update(ctx, sourceConfigMap)
		require.NoError(t, err)

		// Reconcile again
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceConfigMap.Namespace,
			},
		}

		result, err := configMapReconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify synced ConfigMap was updated
		syncedConfigMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      sourceConfigMap.Name,
			Namespace: targetNamespace.Name,
		}, syncedConfigMap)
		require.NoError(t, err)

		assert.Equal(t, "DEBUG", syncedConfigMap.Data["log.level"])
		assert.Equal(t, "value", syncedConfigMap.Data["new.setting"])
	})

	// Test 3: Namespace reconciliation handles new namespace
	t.Run("Namespace reconciliation syncs existing ConfigMaps", func(t *testing.T) {
		// Create a new target namespace
		newNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "staging",
				Annotations: map[string]string{
					SyncTargetAnnotation: "true",
				},
			},
		}
		err := fakeClient.Create(ctx, newNamespace)
		require.NoError(t, err)

		// Reconcile the new namespace
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: newNamespace.Name,
			},
		}

		result, err := namespaceReconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify ConfigMap was synced to new namespace
		syncedConfigMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      sourceConfigMap.Name,
			Namespace: newNamespace.Name,
		}, syncedConfigMap)
		require.NoError(t, err)

		assert.Equal(t, sourceConfigMap.Data, syncedConfigMap.Data)
	})

	// Test 4: Remove sync annotation and verify cleanup
	t.Run("Remove sync annotation cleans up synced ConfigMaps", func(t *testing.T) {
		// Remove sync annotation from source ConfigMap
		err := fakeClient.Get(ctx, types.NamespacedName{
			Name:      sourceConfigMap.Name,
			Namespace: sourceConfigMap.Namespace,
		}, sourceConfigMap)
		require.NoError(t, err)

		delete(sourceConfigMap.Annotations, SyncConfigMapAnnotation)
		err = fakeClient.Update(ctx, sourceConfigMap)
		require.NoError(t, err)

		// Reconcile
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceConfigMap.Namespace,
			},
		}

		result, err := configMapReconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify synced ConfigMaps still exist (they should only be cleaned up on deletion)
		// But no new syncs should happen
		configMapList := &corev1.ConfigMapList{}
		err = fakeClient.List(ctx, configMapList)
		require.NoError(t, err)

		syncedCount := 0
		for _, cm := range configMapList.Items {
			if cm.Namespace != FluxSystemNamespace &&
				cm.Annotations != nil &&
				cm.Annotations[SyncSourceAnnotation] != "" {
				syncedCount++
			}
		}
		assert.Equal(t, 2, syncedCount) // Still have the existing synced copies
	})

	// Test 5: Delete source ConfigMap and verify cleanup
	t.Run("Delete source ConfigMap cleans up all synced copies", func(t *testing.T) {
		// Delete source ConfigMap
		err := fakeClient.Delete(ctx, sourceConfigMap)
		require.NoError(t, err)

		// Reconcile (this should trigger cleanup)
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceConfigMap.Namespace,
			},
		}

		result, err := configMapReconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify all synced ConfigMaps were deleted
		configMapList := &corev1.ConfigMapList{}
		err = fakeClient.List(ctx, configMapList)
		require.NoError(t, err)

		syncedCount := 0
		for _, cm := range configMapList.Items {
			if cm.Namespace != FluxSystemNamespace &&
				cm.Annotations != nil &&
				cm.Annotations[SyncSourceAnnotation] != "" {
				syncedCount++
			}
		}
		assert.Equal(t, 0, syncedCount) // All synced copies should be gone
	})
}

// Test edge cases and error scenarios
func TestConfigMapSync_EdgeCases(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()

	t.Run("ConfigMap in non-flux-system namespace is ignored", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-config",
				Namespace: "other-namespace",
				Annotations: map[string]string{
					SyncConfigMapAnnotation: "true",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(configMap).
			Build()

		reconciler := &ConfigMapReconciler{
			Client: fakeClient,
			Scheme: scheme,
			logger: zap.New(zap.UseDevMode(true)),
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      configMap.Name,
				Namespace: configMap.Namespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Should not have created any synced ConfigMaps
		configMapList := &corev1.ConfigMapList{}
		err = fakeClient.List(ctx, configMapList)
		require.NoError(t, err)
		assert.Len(t, configMapList.Items, 1) // Only the original
	})

	t.Run("Flux-system namespace is ignored by namespace controller", func(t *testing.T) {
		fluxNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: FluxSystemNamespace,
				Annotations: map[string]string{
					SyncTargetAnnotation: "true",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(fluxNamespace).
			Build()

		reconciler := &NamespaceReconciler{
			Client: fakeClient,
			Scheme: scheme,
			logger: zap.New(zap.UseDevMode(true)),
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: FluxSystemNamespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
		// Test passes if no error occurs
	})

	t.Run("Conflicting non-synced ConfigMap is not overwritten", func(t *testing.T) {
		targetNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "conflict-ns",
				Annotations: map[string]string{
					SyncTargetAnnotation: "true",
				},
			},
		}

		sourceConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflict-config",
				Namespace: FluxSystemNamespace,
				Annotations: map[string]string{
					SyncConfigMapAnnotation: "true",
				},
			},
			Data: map[string]string{"key": "source-value"},
		}

		// Existing ConfigMap with same name but different source
		existingConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflict-config",
				Namespace: "conflict-ns",
			},
			Data: map[string]string{"key": "existing-value"},
		}

		objects := []client.Object{targetNamespace, sourceConfigMap, existingConfigMap}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objects...).
			Build()

		reconciler := &ConfigMapReconciler{
			Client: fakeClient,
			Scheme: scheme,
			logger: zap.New(zap.UseDevMode(true)),
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceConfigMap.Namespace,
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify existing ConfigMap was not modified
		conflictConfigMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      "conflict-config",
			Namespace: "conflict-ns",
		}, conflictConfigMap)
		require.NoError(t, err)

		assert.Equal(t, "existing-value", conflictConfigMap.Data["key"])
		assert.Empty(t, conflictConfigMap.Annotations[SyncSourceAnnotation])
	})
}
