# ConfigMap Sync Feature

This document explains how to use the ConfigMap sync feature that allows syncing ConfigMaps from the `flux-system` namespace to target namespaces using annotations.

## Overview

The ConfigMap sync feature enables automatic synchronization of ConfigMaps from the `flux-system` namespace to other namespaces in the cluster. This is useful for sharing configuration data, certificates, or other resources across multiple namespaces while maintaining a single source of truth in the flux-system namespace.

## How It Works

The feature uses two controllers:

1. **ConfigMapReconciler**: Watches ConfigMaps in the `flux-system` namespace and syncs them to target namespaces
2. **NamespaceReconciler**: Watches namespaces and ensures they receive the appropriate synced ConfigMaps

## Annotations

### ConfigMap Annotations (Source)

Add these annotations to ConfigMaps in the `flux-system` namespace:

- `flux-extension.nrfcloud.com/sync-configmap: "true"` - Marks the ConfigMap for syncing
- `flux-extension.nrfcloud.com/sync-configmap/namespaces: "namespace1,namespace2"` - (Optional) Comma-separated list of specific target namespaces

### Namespace Annotations (Target)

Add these annotations to namespaces that should receive synced ConfigMaps:

- `flux-extension.nrfcloud.com/sync-target: "true"` - Marks the namespace as a sync target
- `flux-extension.nrfcloud.com/sync-target/configmaps: "config1,config2"` - (Optional) Comma-separated list of specific ConfigMaps to sync

### Synced ConfigMap Annotations

Synced ConfigMaps will automatically have this annotation:

- `flux-extension.nrfcloud.com/sync-source: "flux-system/original-configmap-name"` - Tracks the source ConfigMap

## Examples

### Example 1: Basic Sync

**ConfigMap in flux-system namespace:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  app.properties: |
    database.url=postgresql://db:5432/myapp
    redis.url=redis://cache:6379
```

**Target namespace:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
```

This will sync the `shared-config` ConfigMap to the `production` namespace.

### Example 2: Selective Sync

**ConfigMap with specific target namespaces:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cert-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
    flux-extension.nrfcloud.com/sync-configmap/namespaces: "staging,production"
data:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
```

**Namespace with specific ConfigMap filter:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: development
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
    flux-extension.nrfcloud.com/sync-target/configmaps: "shared-config,dev-config"
```

### Example 3: Multiple ConfigMaps

**Multiple ConfigMaps in flux-system:**
```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  config.json: |
    {"env": "production", "debug": false}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: logging-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  log4j.properties: |
    log4j.rootLogger=INFO, stdout
```

## Behavior

### Sync Logic

1. **All target namespaces**: If a ConfigMap has no namespace filter, it syncs to all namespaces with `sync-target: "true"`
2. **Specific namespaces**: If a ConfigMap specifies target namespaces, it only syncs to those namespaces
3. **ConfigMap filtering**: If a namespace specifies which ConfigMaps to sync, only those ConfigMaps are synced
4. **Cleanup**: When a ConfigMap is deleted from flux-system, all synced copies are automatically removed
5. **Namespace cleanup**: When a namespace removes the sync annotation, all synced ConfigMaps are removed

### Data Synchronization

- **Data and BinaryData**: Both `data` and `binaryData` fields are synced
- **Annotations**: All annotations except sync-related ones are copied to target ConfigMaps
- **Labels**: Labels are not synced to avoid conflicts
- **Source tracking**: A `sync-source` annotation is added to track the original ConfigMap

### Updates

- When a source ConfigMap is updated, all synced copies are automatically updated
- Only ConfigMaps with the correct `sync-source` annotation are updated (prevents conflicts)

## Security Considerations

1. **RBAC**: Ensure the controller has appropriate permissions to read ConfigMaps in flux-system and create/update/delete ConfigMaps in target namespaces
2. **Sensitive data**: Be careful syncing ConfigMaps containing sensitive information
3. **Namespace isolation**: Consider whether syncing breaks your namespace isolation requirements

## Troubleshooting

### Common Issues

1. **ConfigMap not syncing**: Check that both source and target have correct annotations
2. **Permission errors**: Verify RBAC permissions for the controller
3. **Partial sync**: Check namespace and ConfigMap filters in annotations

### Logs

The controller logs actions with these prefixes:
- `configmap-controller`: ConfigMap sync operations
- `namespace-controller`: Namespace-related sync operations

### Manual Cleanup

If needed, you can manually clean up synced ConfigMaps by looking for the `flux-extension.nrfcloud.com/sync-source` annotation:

```bash
kubectl get configmaps -A -o json | jq -r '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-source"]) | "\(.metadata.namespace)/\(.metadata.name)"'
```
