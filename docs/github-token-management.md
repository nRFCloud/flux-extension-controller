# GitHub Token Management

The Flux Extension Controller provides automatic GitHub App token management for accessing private repositories in Flux CD GitRepository resources. This feature ensures that authentication tokens are automatically created, refreshed, and maintained without manual intervention.

## Overview

This feature enables:
- **Automatic token generation** for GitHub App authentication
- **Token refresh management** to prevent expiration
- **Seamless integration** with Flux CD GitRepository resources
- **Organization-scoped access** for enterprise GitHub setups

## How It Works

The controller monitors GitRepository resources and:

1. **Detects private GitHub repositories** that need authentication
2. **Validates repository URLs** against the configured GitHub organization
3. **Generates GitHub App tokens** using JWT authentication
4. **Creates/updates Kubernetes secrets** with valid tokens
5. **Schedules automatic refresh** before tokens expire
6. **Handles token lifecycle** including cleanup when resources are deleted

## Prerequisites

### GitHub App Setup

1. **Create a GitHub App** in your organization:
   - Go to GitHub → Settings → Developer settings → GitHub Apps
   - Click "New GitHub App"
   - Configure permissions:
     - Repository permissions: `Contents: Read`, `Metadata: Read`
     - Organization permissions: `Members: Read` (if needed)

2. **Generate a private key**:
   - In your GitHub App settings, scroll to "Private keys"
   - Click "Generate a private key"
   - Download and securely store the `.pem` file

3. **Install the GitHub App**:
   - Go to your GitHub App settings
   - Click "Install App"
   - Select your organization and repositories

4. **Note the App ID and Installation ID**:
   - App ID: Found in the GitHub App settings page
   - Installation ID: Found in the URL after installing the app

## Configuration

### Controller Configuration

Configure the GitHub App details in your controller configuration:

```yaml
# config.yaml
github:
   # GitHub App ID (required)
   appId: ""
   privateKeySecret:
      name: "github-app-credentials"
      key: "private-key"
controller:
  organization: "your-org"                         # GitHub organization name

tokenRefresh:
  refreshInterval: "50m"      # How often to check for tokens needing refresh
  tokenLifetime: "1h"         # Expected GitHub App token lifetime
```

### Secret Mounting

Mount the GitHub App private key into the controller pod:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flux-extension-controller
spec:
  template:
    spec:
      containers:
      - name: manager
        volumeMounts:
        - name: github-private-key
          mountPath: /etc/github
          readOnly: true
      volumes:
      - name: github-private-key
        secret:
          secretName: github-app-private-key
          items:
          - key: private-key.pem
            path: private-key.pem
            mode: 0400
```

Create the secret with your private key:

```bash
kubectl create secret generic github-app-private-key \
  --from-file=private-key.pem=/path/to/your/private-key.pem \
  -n flux-system
```

## Usage

### Basic GitRepository with Token Management

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: private-repo
  namespace: flux-system
spec:
  url: https://github.com/your-org/private-repository
  interval: 5m
  secretRef:
    name: github-token-private-repo    # Will be automatically managed
```

The controller will:
1. Detect this is a private repository in your organization
2. Generate a GitHub App token
3. Create the `github-token-private-repo` secret
4. Schedule automatic token refresh

### Multiple Private Repositories

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: app-config
  namespace: flux-system
spec:
  url: https://github.com/your-org/app-config
  interval: 10m
  secretRef:
    name: github-token-app-config

---
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: infrastructure
  namespace: flux-system
spec:
  url: https://github.com/your-org/k8s-infrastructure
  interval: 15m
  secretRef:
    name: github-token-infrastructure
```

Each GitRepository will get its own managed secret with a fresh token.

### Cross-Namespace Repositories

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: team-a-config
  namespace: team-a
spec:
  url: https://github.com/your-org/team-a-config
  interval: 5m
  secretRef:
    name: github-token
```

The controller manages tokens across all namespaces where it has permissions.

## Advanced Configuration

### Custom Token Refresh Timing

```yaml
tokenRefresh:
  refreshInterval: "30m"      # Check every 30 minutes
  tokenLifetime: "55m"        # Assume tokens expire after 55 minutes
```

**Note**: GitHub App tokens typically have a 1-hour lifetime. Configure refresh to happen with sufficient buffer time.

### Organization Validation

The controller validates that repository URLs belong to the configured organization:

```yaml
github:
  organization: "acme-corp"
```

Only repositories under `https://github.com/acme-corp/*` will be processed.

### Installation ID Auto-Detection

If you omit the `installationId`, the controller will attempt to auto-detect it:

```yaml
github:
  appId: 123456
  # installationId: omitted - will be auto-detected
  privateKeyPath: "/etc/github/private-key.pem"
  organization: "your-org"
```

This is useful when the same configuration is used across multiple environments.

## Secret Format

Generated secrets follow this format:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-token-private-repo
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/managed-by: "flux-extension-controller"
    flux-extension.nrfcloud.com/repository-url: "https://github.com/your-org/private-repository"
    flux-extension.nrfcloud.com/token-expires-at: "2025-09-19T10:30:00Z"
type: Opaque
data:
  username: Z2l0aHVi          # base64: "github"
  password: Z2hwX3h4eHh4eA==  # base64: GitHub App token
```

## Monitoring

### Token Status

Check token expiration times:

```bash
kubectl get secrets -A -o json | \
  jq '.items[] | select(.metadata.annotations["flux-extension.nrfcloud.com/managed-by"] == "flux-extension-controller") | 
      {name: .metadata.name, namespace: .metadata.namespace, expires: .metadata.annotations["flux-extension.nrfcloud.com/token-expires-at"]}'
```

### Refresh Operations

Monitor token refresh operations in controller logs:

```bash
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "token refresh"
```

### GitRepository Status

Check if GitRepositories can access their repositories:

```bash
kubectl get gitrepositories -A -o wide
```

Look for `Ready=True` status and recent successful reconciliations.

## Troubleshooting

### Common Issues

**GitRepository stuck in "Authentication failed" state:**

1. **Check GitHub App configuration**:
   ```bash
   # Verify the secret exists
   kubectl get secret github-token-<name> -n <namespace>
   
   # Check token expiration
   kubectl get secret github-token-<name> -n <namespace> -o json | \
     jq '.metadata.annotations["flux-extension.nrfcloud.com/token-expires-at"]'
   ```

2. **Verify repository URL format**:
   - Must be `https://github.com/organization/repository`
   - Must match the configured organization
   - Repository must exist and be accessible by the GitHub App

3. **Check GitHub App permissions**:
   - Contents: Read
   - Metadata: Read
   - App must be installed on the target repository

**Tokens not refreshing automatically:**

1. **Check refresh configuration**:
   ```bash
   kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "refresh"
   ```

2. **Verify token lifetime settings**:
   - `refreshInterval` should be less than `tokenLifetime`
   - `tokenLifetime` should be less than actual GitHub token expiration (usually 1 hour)

**Permission errors:**

1. **Verify RBAC permissions**:
   ```bash
   kubectl auth can-i create secrets --as=system:serviceaccount:flux-system:flux-extension-controller
   kubectl auth can-i update secrets --as=system:serviceaccount:flux-system:flux-extension-controller
   kubectl auth can-i get gitrepositories --as=system:serviceaccount:flux-system:flux-extension-controller
   ```

2. **Check GitHub App installation**:
   - Verify the app is installed on the target repositories
   - Check organization-level permissions if using org-owned repositories

### Debug Commands

```bash
# List all managed secrets
kubectl get secrets -A -l flux-extension.nrfcloud.com/managed-by=flux-extension-controller

# Check specific secret details
kubectl describe secret github-token-<name> -n <namespace>

# View controller events
kubectl get events -n flux-system --field-selector involvedObject.kind=Secret

# Test GitHub API access (manual verification)
# Replace <token> with actual token from secret
curl -H "Authorization: Bearer <token>" \
     -H "Accept: application/vnd.github.v3+json" \
     https://api.github.com/repos/your-org/your-repo
```

### Log Analysis

Look for these log patterns:

```bash
# Successful token generation
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "token generated"

# Token refresh operations
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "refreshing token"

# Authentication errors
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "authentication failed"

# Repository validation
kubectl logs -n flux-system -l app.kubernetes.io/name=flux-extension-controller | grep "repository validation"
```

## Security Considerations

### Private Key Security

- **Store private keys in Kubernetes secrets** with restricted access
- **Use `mode: 0400`** when mounting private key files
- **Rotate private keys periodically** as per your security policy
- **Monitor access** to secrets containing private keys

### Token Security

- **Tokens are stored in Kubernetes secrets** with base64 encoding
- **Limit secret access** using RBAC
- **Monitor token usage** through GitHub audit logs
- **Implement network policies** to restrict controller access

### GitHub App Permissions

- **Use minimal required permissions**:
  - Contents: Read (for repository access)
  - Metadata: Read (for repository information)
- **Avoid organization-level permissions** unless necessary
- **Regularly audit** GitHub App installations and permissions

## Best Practices

1. **Use unique secret names** for each GitRepository to avoid conflicts
2. **Monitor token expiration** and refresh operations
3. **Set appropriate refresh intervals** with sufficient buffer time
4. **Implement monitoring** for authentication failures
5. **Document GitHub App setup** for your team
6. **Test token refresh** in non-production environments first
7. **Keep private keys secure** and rotate them regularly
8. **Use organization-scoped** GitHub Apps for better security

## Limitations

- Only supports GitHub.com repositories (not GitHub Enterprise Server)
- Requires GitHub App setup and private key management
- Token lifetime is limited by GitHub (typically 1 hour)
- Repository URLs must exactly match the configured organization
- One GitHub organization per controller instance
