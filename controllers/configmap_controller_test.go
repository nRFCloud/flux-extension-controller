package controllers

import (
	"context"
	"testing"

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
)

func TestConfigMapReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name           string
		configMap      *corev1.ConfigMap
		namespaces     []*corev1.Namespace
		expectedSynced int
		shouldError    bool
	}{
		{
			name: "sync configmap to all target namespaces",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: FluxSystemNamespace,
					Annotations: map[string]string{
						SyncConfigMapAnnotation: "true",
					},
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-1",
						Annotations: map[string]string{
							SyncTargetAnnotation: "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-2",
						Annotations: map[string]string{
							SyncTargetAnnotation: "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "no-sync-ns",
					},
				},
			},
			expectedSynced: 2,
		},
		{
			name: "sync configmap to specific namespaces",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "specific-config",
					Namespace: FluxSystemNamespace,
					Annotations: map[string]string{
						SyncConfigMapAnnotation:                                 "true",
						"flux-extension.nrfcloud.com/sync-configmap-namespaces": "target-ns-1,target-ns-3",
					},
				},
				Data: map[string]string{
					"app.properties": "database.url=localhost",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-1",
						Annotations: map[string]string{
							SyncTargetAnnotation: "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-2",
						Annotations: map[string]string{
							SyncTargetAnnotation: "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-3",
					},
				},
			},
			expectedSynced: 2, // target-ns-1 and target-ns-3
		},
		{
			name: "no sync annotation - should not sync",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-sync-config",
					Namespace: FluxSystemNamespace,
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "target-ns-1",
						Annotations: map[string]string{
							SyncTargetAnnotation: "true",
						},
					},
				},
			},
			expectedSynced: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create objects for the fake client
			objects := []client.Object{}
			if tt.configMap != nil {
				objects = append(objects, tt.configMap)
			}
			for _, ns := range tt.namespaces {
				objects = append(objects, ns)
			}

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
					Name:      tt.configMap.Name,
					Namespace: tt.configMap.Namespace,
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			if tt.shouldError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			// Check that the expected number of ConfigMaps were synced
			configMapList := &corev1.ConfigMapList{}
			err = fakeClient.List(ctx, configMapList)
			require.NoError(t, err)

			syncedCount := 0
			for _, cm := range configMapList.Items {
				if cm.Namespace != FluxSystemNamespace &&
					cm.Annotations != nil &&
					cm.Annotations[SyncSourceAnnotation] != "" {
					syncedCount++

					// Verify the synced ConfigMap has correct data
					assert.Equal(t, tt.configMap.Data, cm.Data)
					assert.Equal(t, tt.configMap.Name, cm.Name)
					expectedSource := FluxSystemNamespace + "/" + tt.configMap.Name
					assert.Equal(t, expectedSource, cm.Annotations[SyncSourceAnnotation])
				}
			}

			assert.Equal(t, tt.expectedSynced, syncedCount)
		})
	}
}

func TestConfigMapReconciler_shouldSyncConfigMap(t *testing.T) {
	reconciler := &ConfigMapReconciler{}

	tests := []struct {
		name      string
		configMap *corev1.ConfigMap
		expected  bool
	}{
		{
			name: "with sync annotation true",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						SyncConfigMapAnnotation: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "with sync annotation false",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						SyncConfigMapAnnotation: "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "without sync annotation",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			name: "no annotations",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldSyncConfigMap(tt.configMap)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigMapReconciler_shouldReceiveSync(t *testing.T) {
	reconciler := &ConfigMapReconciler{}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-config",
		},
	}

	tests := []struct {
		name      string
		namespace *corev1.Namespace
		expected  bool
	}{
		{
			name: "namespace with sync target annotation",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "namespace with specific configmap filter - matches",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
						"flux-extension.nrfcloud.com/sync-target-configmaps": "test-config,other-config",
					},
				},
			},
			expected: true,
		},
		{
			name: "namespace with specific configmap filter - no match",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
						"flux-extension.nrfcloud.com/sync-target-configmaps": "other-config,another-config",
					},
				},
			},
			expected: false,
		},
		{
			name: "flux-system namespace should be skipped",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: FluxSystemNamespace,
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
					},
				},
			},
			expected: false,
		},
		{
			name: "namespace without sync annotation",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-sync-ns",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldReceiveSync(tt.namespace, configMap)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigMapReconciler_cleanupSyncedConfigMaps(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()
	configMapName := "deleted-config"

	// Create synced ConfigMaps in different namespaces
	syncedConfigMaps := []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: "target-ns-1",
				Annotations: map[string]string{
					SyncSourceAnnotation: FluxSystemNamespace + "/" + configMapName,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: "target-ns-2",
				Annotations: map[string]string{
					SyncSourceAnnotation: FluxSystemNamespace + "/" + configMapName,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-config",
				Namespace: "target-ns-1",
				Annotations: map[string]string{
					SyncSourceAnnotation: FluxSystemNamespace + "/other-config",
				},
			},
		},
	}

	objects := []client.Object{}
	for _, cm := range syncedConfigMaps {
		objects = append(objects, cm)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	reconciler := &ConfigMapReconciler{
		Client: fakeClient,
		Scheme: scheme,
		logger: zap.New(zap.UseDevMode(true)),
	}

	result, err := reconciler.cleanupSyncedConfigMaps(ctx, configMapName, reconciler.logger)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that only the synced ConfigMaps with the correct source were deleted
	configMapList := &corev1.ConfigMapList{}
	err = fakeClient.List(ctx, configMapList)
	require.NoError(t, err)

	// Should only have the "other-config" remaining
	assert.Len(t, configMapList.Items, 1)
	assert.Equal(t, "other-config", configMapList.Items[0].Name)
}
