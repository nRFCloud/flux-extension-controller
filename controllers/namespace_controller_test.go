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

func TestNamespaceReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name           string
		namespace      *corev1.Namespace
		configMaps     []*corev1.ConfigMap
		expectedSynced int
		shouldError    bool
	}{
		{
			name: "sync all configmaps to target namespace",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
					},
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-1",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key1": "value1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-2",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key2": "value2"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-sync-config",
						Namespace: FluxSystemNamespace,
					},
					Data: map[string]string{"key3": "value3"},
				},
			},
			expectedSynced: 2,
		},
		{
			name: "sync specific configmaps based on namespace filter",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "filtered-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation:                 "true",
						SyncTargetAnnotation + "/configmaps": "config-1,config-3",
					},
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-1",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key1": "value1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-2",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key2": "value2"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-3",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key3": "value3"},
				},
			},
			expectedSynced: 2, // config-1 and config-3
		},
		{
			name: "namespace without sync annotation - should cleanup",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-sync-ns",
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config-1",
						Namespace: FluxSystemNamespace,
						Annotations: map[string]string{
							SyncConfigMapAnnotation: "true",
						},
					},
					Data: map[string]string{"key1": "value1"},
				},
			},
			expectedSynced: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create objects for the fake client
			objects := []client.Object{tt.namespace}
			for _, cm := range tt.configMaps {
				objects = append(objects, cm)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &NamespaceReconciler{
				Client: fakeClient,
				Scheme: scheme,
				logger: zap.New(zap.UseDevMode(true)),
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: tt.namespace.Name,
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
			err = fakeClient.List(ctx, configMapList, client.InNamespace(tt.namespace.Name))
			require.NoError(t, err)

			syncedCount := 0
			for _, cm := range configMapList.Items {
				if cm.Annotations != nil && cm.Annotations[SyncSourceAnnotation] != "" {
					syncedCount++
				}
			}

			assert.Equal(t, tt.expectedSynced, syncedCount)
		})
	}
}

func TestNamespaceReconciler_shouldReceiveSync(t *testing.T) {
	reconciler := &NamespaceReconciler{}

	tests := []struct {
		name      string
		namespace *corev1.Namespace
		expected  bool
	}{
		{
			name: "namespace with sync target annotation true",
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
			name: "namespace with sync target annotation false",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "false",
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
		{
			name: "namespace with no annotations",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "empty-ns",
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldReceiveSync(tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNamespaceReconciler_shouldSyncToNamespace(t *testing.T) {
	reconciler := &NamespaceReconciler{}

	tests := []struct {
		name      string
		namespace *corev1.Namespace
		configMap *corev1.ConfigMap
		expected  bool
	}{
		{
			name: "no filters - should sync",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation: "true",
					},
				},
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
			},
			expected: true,
		},
		{
			name: "namespace filter matches",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation:                 "true",
						SyncTargetAnnotation + "/configmaps": "test-config,other-config",
					},
				},
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
			},
			expected: true,
		},
		{
			name: "namespace filter no match",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
					Annotations: map[string]string{
						SyncTargetAnnotation:                 "true",
						SyncTargetAnnotation + "/configmaps": "other-config,another-config",
					},
				},
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
			},
			expected: false,
		},
		{
			name: "configmap filter matches",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
				},
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
					Annotations: map[string]string{
						SyncConfigMapAnnotation:                 "true",
						SyncConfigMapAnnotation + "/namespaces": "target-ns,other-ns",
					},
				},
			},
			expected: true,
		},
		{
			name: "configmap filter no match",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "target-ns",
				},
			},
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
					Annotations: map[string]string{
						SyncConfigMapAnnotation:                 "true",
						SyncConfigMapAnnotation + "/namespaces": "other-ns,another-ns",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldSyncToNamespace(tt.namespace, tt.configMap)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNamespaceReconciler_cleanupSyncedConfigMapsInNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()
	namespaceName := "cleanup-ns"

	// Create ConfigMaps in the namespace, some synced, some not
	configMaps := []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "synced-config-1",
				Namespace: namespaceName,
				Annotations: map[string]string{
					SyncSourceAnnotation: FluxSystemNamespace + "/original-config-1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "synced-config-2",
				Namespace: namespaceName,
				Annotations: map[string]string{
					SyncSourceAnnotation: FluxSystemNamespace + "/original-config-2",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "native-config",
				Namespace: namespaceName,
			},
		},
	}

	objects := []client.Object{}
	for _, cm := range configMaps {
		objects = append(objects, cm)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	reconciler := &NamespaceReconciler{
		Client: fakeClient,
		Scheme: scheme,
		logger: zap.New(zap.UseDevMode(true)),
	}

	result, err := reconciler.cleanupSyncedConfigMapsInNamespace(ctx, namespaceName, reconciler.logger)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that only synced ConfigMaps were deleted
	configMapList := &corev1.ConfigMapList{}
	err = fakeClient.List(ctx, configMapList, client.InNamespace(namespaceName))
	require.NoError(t, err)

	// Should only have the "native-config" remaining
	assert.Len(t, configMapList.Items, 1)
	assert.Equal(t, "native-config", configMapList.Items[0].Name)
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		sep      string
		expected []string
	}{
		{
			name:     "basic split",
			input:    "a,b,c",
			sep:      ",",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "split with spaces",
			input:    "a, b , c",
			sep:      ",",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty string",
			input:    "",
			sep:      ",",
			expected: nil,
		},
		{
			name:     "single item",
			input:    "single",
			sep:      ",",
			expected: []string{"single"},
		},
		{
			name:     "empty items filtered out",
			input:    "a,, ,b",
			sep:      ",",
			expected: []string{"a", "b"},
		},
		{
			name:     "with tabs and newlines",
			input:    "a,\tb\n, \tc ",
			sep:      ",",
			expected: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitAndTrim(tt.input, tt.sep)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrimString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic trim",
			input:    "  hello  ",
			expected: "hello",
		},
		{
			name:     "tabs and newlines",
			input:    "\t\nhello\n\t",
			expected: "hello",
		},
		{
			name:     "no trim needed",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   \t\n  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
