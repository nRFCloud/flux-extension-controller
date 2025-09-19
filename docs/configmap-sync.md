# ConfigMap Synchronization

The Flux Extension Controller provides automatic ConfigMap synchronization from the `flux-system` namespace to target namespaces, enabling centralized configuration management across your Kubernetes cluster.

## Overview

This feature allows you to:
- Define configuration once in the `flux-system` namespace
- Automatically distribute it to multiple namespaces
- Maintain consistency across environments
- Simplify configuration management for multi-tenant clusters

## How It Works

The controller watches for:
1. **ConfigMaps** in the `flux-system` namespace with the sync annotation
2. **Namespaces** with the sync target annotation
3. **Changes** to either source ConfigMaps or target namespaces

When a ConfigMap is marked for syncing, the controller automatically creates and maintains copies in all target namespaces.

## Configuration

### Marking ConfigMaps for Synchronization

Add the sync annotation to ConfigMaps in the `flux-system` namespace:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  database.host: "db.example.com"
  database.port: "5432"
  api.version: "v1"
  log.level: "info"
```

### Marking Target Namespaces

Add the sync target annotation to namespaces that should receive synced ConfigMaps:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
```

## Behavior

### Automatic Synchronization

- **Creation**: When a ConfigMap is marked for sync, it's immediately copied to all target namespaces
- **Updates**: Changes to source ConfigMaps are automatically propagated to all copies
- **Deletion**: When a source ConfigMap is deleted, all synced copies are removed
- **Namespace Addition**: When a new namespace is marked as a target, it receives all synced ConfigMaps
- **Namespace Removal**: When a namespace loses the target annotation, synced ConfigMaps are cleaned up

### Source Tracking

Synced ConfigMaps include a source annotation for tracking:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: production
  annotations:
    flux-extension.nrfcloud.com/sync-source: "flux-system/shared-config"
data:
  # ... same data as source ConfigMap
```

## Examples

### Basic Usage

1. **Create a shared configuration:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  environment: "production"
  debug: "false"
  timeout: "30s"
```

2. **Mark target namespaces:**

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: app-frontend
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
---
apiVersion: v1
kind: Namespace
metadata:
  name: app-backend
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
```

3. **Result**: The `app-settings` ConfigMap will be automatically created in both `app-frontend` and `app-backend` namespaces.

### Multi-Environment Setup

```yaml
# Shared base configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: base-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  app.name: "myapp"
  app.version: "1.0.0"
  monitoring.enabled: "true"

---
# Environment-specific configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: db-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  database.host: "postgres.example.com"
  database.name: "myapp_prod"
  database.ssl: "true"

---
# Target namespaces
apiVersion: v1
kind: Namespace
metadata:
  name: production
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
---
apiVersion: v1
kind: Namespace
metadata:
  name: staging
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
```

Both ConfigMaps will be synced to both target namespaces.

### Using Synced ConfigMaps in Applications

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        envFrom:
        - configMapRef:
            name: base-config    # Synced from flux-system
        - configMapRef:
            name: db-config     # Synced from flux-system
        env:
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
```

## Advanced Configuration

### Selective Synchronization

You can control which ConfigMaps are synced by only adding the annotation to specific ConfigMaps:

```yaml
# This ConfigMap will be synced
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  shared.key: "value"

---
# This ConfigMap will NOT be synced (no annotation)
apiVersion: v1
kind: ConfigMap
metadata:
  name: internal-config
  namespace: flux-system
data:
  internal.key: "value"
```

### Namespace Exclusion

Configure the controller to exclude certain namespaces:

```yaml
# config.yaml
controller:
  excludedNamespaces: 
    - "kube-system"
    - "kube-public"
    - "local-path-storage"
```

Excluded namespaces will never receive synced ConfigMaps, even if they have the target annotation.

## Monitoring

### Checking Sync Status

List all synced ConfigMaps:

```bash
kubectl get configmaps -A -o json | \
  jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-source"]) | 
      {namespace: .metadata.namespace, name: .metadata.name, source: .metadata.annotations["flux-extension.nrfcloud.com/sync-source"]}'
```

Check specific namespace:

```bash
kubectl get configmaps -n production -o json | \
  jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-source"])'
```

### Controller Logs

Monitor sync operations:

```bash
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep configmap
```

## Troubleshooting

### Common Issues

**ConfigMaps not appearing in target namespaces:**

1. Verify the source ConfigMap has the sync annotation:
   ```bash
   kubectl get configmap -n flux-system <name> -o yaml | grep flux-extension.nrfcloud.com/sync-configmap
   ```

2. Verify target namespaces have the target annotation:
   ```bash
   kubectl get namespace <name> -o yaml | grep flux-extension.nrfcloud.com/sync-target
   ```

3. Check controller logs for errors:
   ```bash
   kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller
   ```

**Synced ConfigMaps not updating:**

1. Verify the source ConfigMap was actually updated
2. Check controller logs for reconciliation errors
3. Ensure the controller has proper RBAC permissions

**Permission errors:**

The controller needs these permissions:
- `configmaps`: `get`, `list`, `watch`, `create`, `update`, `patch`, `delete`
- `namespaces`: `get`, `list`, `watch`

### Debug Commands

```bash
# List all source ConfigMaps marked for sync
kubectl get configmaps -n flux-system -o json | \
  jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-configmap"] == "true")'

# List all target namespaces
kubectl get namespaces -o json | \
  jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-target"] == "true")'

# Check controller events
kubectl get events -n flux-system --field-selector involvedObject.kind=ConfigMap

# Describe controller deployment
kubectl describe deployment -n flux-system flux-extension-controller
```

## Best Practices

1. **Use descriptive names** for ConfigMaps that will be synced
2. **Keep sensitive data in Secrets**, not ConfigMaps
3. **Use namespaced resources** when possible instead of cluster-wide sync
4. **Monitor sync operations** through logs and metrics
5. **Test changes** in non-production namespaces first
6. **Document** which ConfigMaps are synced and why

## Limitations

- Only ConfigMaps in the `flux-system` namespace can be marked for sync
- Synced ConfigMaps are read-only in target namespaces (managed by the controller)
- Large ConfigMaps may impact cluster performance during sync operations
- Binary data in ConfigMaps is supported but may increase memory usage
