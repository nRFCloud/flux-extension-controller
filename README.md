# Flux Extension Controller

A Kubernetes controller that extends Flux CD functionality with additional features.

## Features

### 🔐 GitHub App Token Management
Manages GitHub App authentication tokens for private repositories, automatically refreshing tokens before they expire to ensure uninterrupted access to private Git repositories.

**[📖 Full GitHub Token Management Documentation](docs/github-token-management.md)**

### 🔄 ConfigMap Synchronization
Automatically synchronizes ConfigMaps from the `flux-system` namespace to target namespaces, enabling centralized configuration management across your cluster.

**[📖 Full ConfigMap Sync Documentation](docs/configmap-sync.md)**

### 🎯 Namespace-aware Operations
Provides namespace-level control over which namespaces participate in ConfigMap synchronization and token management.

## Installation

### Prerequisites

- Kubernetes cluster (v1.25+)
- Flux CD installed and configured
- Helm 3.x
- GitHub App configured for repository access (for private repos - see [GitHub Token Management docs](docs/github-token-management.md))

### Install via Helm (Recommended)

#### 1. Add the Helm Repository

```bash
helm repo add flux-extension https://nrfcloud.github.io/flux-extension-controller
helm repo update
```

#### 2. Basic Installation (ConfigMap Sync Only)

For ConfigMap synchronization without GitHub integration:

```bash
helm install flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --create-namespace
```

#### 3. Installation with GitHub App Integration

For private repository access, you'll need to configure GitHub App credentials:

**Step 1**: Create the GitHub App private key secret
```bash
kubectl create secret generic github-app-private-key \
  --from-file=private-key.pem=/path/to/your/github-app-private-key.pem \
  --namespace flux-system
```

**Step 2**: Install with GitHub configuration
```bash
helm install flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --create-namespace \
  --set github.appId=123456 \
  --set github.organization="your-org"
```

#### 4. Custom Installation with Values File

Create a `values.yaml` file:

```yaml
# values.yaml
github:
  appId: 123456
  privateKeySecret:
     name: "github-app-credentials"
     key: "private-key"

controller:
  organization: "your-org"
  excludedNamespaces:
    - "kube-system"
    - "kube-public"
    - "monitoring"
  tokenRefresh:
    refreshInterval: "45m"
    tokenLifetime: "60m"

replicaCount: 2

resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 128Mi
```

Install with custom values:
```bash
helm install flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --create-namespace \
  --values values.yaml
```

### Alternative: Install from Source

For development or customization:

```bash
git clone https://github.com/nrfcloud/flux-extension-controller.git
cd flux-extension-controller
helm install flux-extension ./chart \
  --namespace flux-system \
  --create-namespace \
  --values ./chart/values.yaml
```

### Upgrade

```bash
helm repo update
helm upgrade flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system
```

### Uninstall

```bash
helm uninstall flux-extension --namespace flux-system
```

## Quick Start Examples

### Example 1: Enable ConfigMap Synchronization

After installation, configure ConfigMap sync:

```bash
# Mark a ConfigMap for syncing
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  app.name: "myapp"
  log.level: "info"
EOF

# Mark a namespace to receive synced ConfigMaps
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: production
  annotations:
    flux-extension.nrfcloud.com/sync-target: "true"
EOF
```

The ConfigMap will automatically appear in the `production` namespace.

### Example 2: Private Repository Access

After installation with GitHub configuration:

```bash
kubectl apply -f - <<EOF
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: private-repo
  namespace: flux-system
spec:
  url: https://github.com/your-org/private-repo
  interval: 5m
  secretRef:
    name: github-token  # Automatically managed
EOF
```

The controller will create and manage the `github-token` secret automatically.

## Configuration Options

### Helm Values Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `github.appId` | GitHub App ID for private repos | `""` |
| `controller.organization` | GitHub organization name | `""` |
| `controller.excludedNamespaces` | Namespaces to exclude | `["kube-system", "kube-public", "kube-node-lease"]` |
| `controller.watchAllNamespaces` | Watch all namespaces | `true` |
| `controller.tokenRefresh.refreshInterval` | Token refresh interval | `"50m"` |
| `controller.tokenRefresh.tokenLifetime` | Expected token lifetime | `"1h"` |
| `replicaCount` | Number of controller replicas | `1` |
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus | `false` |
| `podDisruptionBudget.enabled` | Enable PodDisruptionBudget | `true` |

### Complete Configuration Example

```yaml
# Complete values.yaml example
github:
  appId: 123456
  privateKeySecret:
     name: "github-app-credentials"
     key: "private-key"

controller:
  organization: "acme-corp"
  excludedNamespaces:
    - "kube-system"
    - "kube-public"
    - "monitoring"
    - "istio-system"
  watchAllNamespaces: true
  tokenRefresh:
    refreshInterval: "55m"
    tokenLifetime: "60m"

replicaCount: 2

image:
  repository: ghcr.io/nrfcloud/flux-extension-controller
  tag: "v0.0.0"
  pullPolicy: IfNotPresent

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

## Monitoring and Observability

### Metrics

The controller exposes Prometheus metrics on the configured metrics address (default `:8080`):

- `controller_runtime_*` - Standard controller-runtime metrics
- Custom metrics for sync operations and token refresh activities

#### Enable Prometheus Monitoring

```bash
helm upgrade flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.serviceMonitor.additionalLabels.prometheus=kube-prometheus
```

### Health Checks

Health and readiness probes are available on the configured health probe address (default `:8081`):

- `/healthz` - Health check endpoint
- `/readyz` - Readiness check endpoint

### Logging

Configure logging levels via Helm values:

```yaml
# values.yaml
podAnnotations:
  logging.level: "debug"
```

Or set during installation:
```bash
helm install flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --set podAnnotations."logging\.level"="debug"
```

## Development

### Local Development with Helm

```bash
# Clone the repository
git clone https://github.com/nrfcloud/flux-extension-controller.git
cd flux-extension-controller

# Build and install locally
make docker-build
helm install flux-extension ./chart \
  --namespace flux-system \
  --create-namespace \
  --set image.tag=latest \
  --set image.pullPolicy=Never
```

### Testing Helm Chart

```bash
# Lint the chart
helm lint ./chart

# Template and validate
helm template flux-extension ./chart --debug

# Dry run installation
helm install flux-extension ./chart \
  --namespace flux-system \
  --dry-run --debug
```

## Troubleshooting

### Helm-Specific Issues

**Installation fails with RBAC errors:**
```bash
# Check if you have cluster-admin permissions
kubectl auth can-i create clusterroles
kubectl auth can-i create clusterrolebindings

# Install with custom RBAC if needed
helm install flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --set rbac.create=false
```

**Controller not starting:**
```bash
# Check Helm release status
helm status flux-extension -n flux-system

# View deployment logs
kubectl logs -n flux-system deployment/flux-extension-controller

# Check pod events
kubectl describe pod -n flux-system -l app.kubernetes.io/name=flux-extension-controller
```

**Configuration issues:**
```bash
# View rendered configuration
kubectl get configmap -n flux-system flux-extension-controller -o yaml

# Test configuration changes
helm upgrade flux-extension flux-extension/flux-extension-controller \
  --namespace flux-system \
  --dry-run --debug
```

### Common Debug Commands

```bash
# Check Helm release
helm list -n flux-system

# View all created resources
helm get manifest flux-extension -n flux-system

# Check controller status
kubectl get deployment -n flux-system flux-extension-controller

# View controller logs
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller --tail=100

# Check ConfigMap sync status
kubectl get configmaps -A -o json | jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/sync-source"])'

# Check GitRepository status
kubectl get gitrepositories -A
```

### Rollback

If you encounter issues after an upgrade:

```bash
# View release history
helm history flux-extension -n flux-system

# Rollback to previous version
helm rollback flux-extension -n flux-system

# Rollback to specific revision
helm rollback flux-extension 1 -n flux-system
```

## Configuration Reference

### Complete Helm Values Schema

For the complete list of available configuration options, see the [chart values.yaml](chart/values.yaml) file or run:

```bash
helm show values flux-extension/flux-extension-controller
```

### Annotations Reference

| Annotation | Target | Purpose |
|------------|---------|----------|
| `flux-extension.nrfcloud.com/sync-configmap` | ConfigMap | Mark ConfigMap for synchronization |
| `flux-extension.nrfcloud.com/sync-target` | Namespace | Mark namespace to receive synced ConfigMaps |
| `flux-extension.nrfcloud.com/sync-source` | ConfigMap | Track source of synced ConfigMaps (auto-added) |

## Migration from Manual Installation

If you previously installed using raw manifests:

1. **Backup your configuration:**
   ```bash
   kubectl get configmap -n flux-system flux-extension-controller -o yaml > backup-config.yaml
   kubectl get secret -n flux-system github-app-private-key -o yaml > backup-secret.yaml
   ```

2. **Remove manual installation:**
   ```bash
   kubectl delete deployment -n flux-system flux-extension-controller
   kubectl delete configmap -n flux-system flux-extension-controller
   kubectl delete serviceaccount -n flux-system flux-extension-controller
   # ... remove other manually created resources
   ```

3. **Install via Helm:**
   ```bash
   helm install flux-extension flux-extension/flux-extension-controller \
     --namespace flux-system \
     --set github.appId=YOUR_APP_ID \
     --set github.organization="your-org"
   ```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test with Helm: `helm lint ./chart && helm template ./chart`
5. Ensure all tests pass: `make test`
6. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

- **Documentation:** [docs/](docs/)
- **Helm Chart:** [chart/](chart/)
- **Issues:** [GitHub Issues](https://github.com/nrfcloud/flux-extension-controller/issues)
- **Discussions:** [GitHub Discussions](https://github.com/nrfcloud/flux-extension-controller/discussions)
