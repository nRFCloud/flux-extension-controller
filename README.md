# Flux Extension Controller

A Kubernetes controller that automatically manages GitHub App installation tokens for Flux GitRepository resources pointing to github.com/nrfcloud repositories.

## Features

- **Automatic Token Management**: Generates and refreshes GitHub App installation tokens for GitRepository resources
- **Repository Validation**: Ensures repositories belong to the configured GitHub organization (nrfcloud by default)
- **Token Refresh**: Automatically refreshes tokens before expiration (~50 minutes) to maintain continuous access
- **Namespace Filtering**: Configurable namespace exclusions (flux-system excluded by default)
- **Extensible Architecture**: Plugin-ready design for future Flux-related automation tasks
- **Security**: Uses GitHub App authentication with short-lived tokens (max 1-hour lifetime)

## Architecture

The controller watches GitRepository custom resources across all namespaces and:

1. Validates repository URLs belong to the configured GitHub organization
2. Uses GitHub App credentials to generate installation tokens
3. Creates/updates Kubernetes secrets with the generated tokens
4. Schedules automatic token refresh before expiration
5. Updates GitRepository status with operation results

### Components

- **GitRepository Controller**: Main reconciliation logic
- **GitHub Client**: Handles GitHub App authentication and token generation
- **Secret Manager**: Manages Kubernetes secrets for Git credentials
- **Token Refresh Manager**: Schedules and executes token refresh operations
- **Configuration Manager**: Handles app configuration and credentials

## Prerequisites

- Kubernetes cluster with Flux Source Controller installed
- GitHub App with repository access permissions
- GitHub App private key stored in a Kubernetes secret

## Installation

### 1. Install Flux Source Controller CRDs

```bash
kubectl apply -f https://github.com/fluxcd/source-controller/releases/latest/download/source-controller.crds.yaml
```

### 2. Create GitHub App Credentials Secret

```bash
kubectl create secret generic github-app-credentials \
  --from-file=private-key=/path/to/your/github-app-private-key.pem \
  --namespace flux-extension-controller
```

### 3. Install with Helm

```bash
helm upgrade --install flux-extension-controller ./chart \
  --namespace flux-extension-controller \
  --create-namespace \
  --set github.appId=YOUR_GITHUB_APP_ID \
  --wait
```

## Configuration

### Values.yaml Configuration

```yaml
# GitHub App configuration
github:
  appId: "123456"  # Your GitHub App ID
  privateKeySecret:
    name: "github-app-credentials"
    key: "private-key"

# Controller settings
controller:
  organization: "nrfcloud"  # GitHub organization to manage
  excludedNamespaces:
    - "flux-system"
    - "kube-system"
  tokenRefresh:
    refreshInterval: "50m"
    tokenLifetime: "60m"

# Node scheduling (similar to Karpenter setup)
nodeSelector:
  kubernetes.io/arch: amd64

tolerations:
  - key: "node-role.kubernetes.io/control-plane"
    operator: "Exists"
    effect: "NoSchedule"
```

### Environment Variables

- `GITHUB_APP_ID`: GitHub App ID
- `GITHUB_PRIVATE_KEY_PATH`: Path to GitHub App private key
- `GITHUB_ORGANIZATION`: GitHub organization (default: nrfcloud)

## Usage

### GitRepository Example

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: my-repo
  namespace: default
spec:
  url: https://github.com/nrfcloud/my-repository
  ref:
    branch: main
  interval: 1m
  secretRef:
    name: my-repo-credentials  # Controller will create/manage this secret
```

The controller will:
1. Detect the GitRepository targets github.com/nrfcloud
2. Generate a GitHub App installation token
3. Create the `my-repo-credentials` secret with the token
4. Schedule automatic token refresh
5. Update the GitRepository status

## Development

### Building

```bash
# Build the binary
make build

# Build Docker image
make docker-build

# Run tests
make test

# Run locally (requires kubeconfig)
make run
```

### Multi-architecture Build

```bash
make docker-buildx
```

## Security Considerations

- GitHub App private keys are stored in Kubernetes secrets
- Tokens have a maximum 1-hour lifetime
- Controller runs with minimal RBAC permissions
- Secrets are managed with proper ownership references
- Non-root container with read-only filesystem

## Monitoring

The controller exposes Prometheus metrics on port 8080:

- GitRepository reconciliation metrics
- Token generation and refresh statistics
- Error rates and latencies

## Troubleshooting

### Common Issues

1. **GitHub App Authentication Failed**
   - Verify GitHub App ID and private key
   - Check App installation on target repositories

2. **Secret Already Exists**
   - Controller validates secret ownership
   - Existing secrets not managed by controller will cause errors

3. **Token Refresh Failures**
   - Check GitHub App permissions
   - Verify network connectivity to GitHub API

### Logs

```bash
kubectl logs -n flux-extension-controller deployment/flux-extension-controller
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.

