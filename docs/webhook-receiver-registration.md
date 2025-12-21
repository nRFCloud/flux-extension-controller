# GitHub Webhook Registration for Flux Receivers

## Overview

This feature extends the Flux Extension Controller to automatically register GitHub webhooks for Flux Receiver resources. It enables automatic webhook configuration for repositories managed by the controller, eliminating manual webhook setup and ensuring consistent webhook configuration across multiple receivers.

## Objectives

- **Automatic webhook registration**: Create and manage GitHub webhooks for Receiver resources
- **Webhook token management**: Generate and manage webhook secrets for authentication
- **Selective operation**: Only act on Receivers that reference GitRepositories already managed by the extension controller
- **Event configuration**: Configure webhooks to send only the events specified in the Receiver spec
- **Centralized configuration**: Configure webhook base URL in the controller configuration

## How It Works

### Resource Relationships

The feature operates on the following Flux CD resources:

```yaml
# GitRepository (managed by existing controller)
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: webapp
  namespace: default
spec:
  url: https://github.com/your-org/webapp
  interval: 5m
  secretRef:
    name: github-token

---
# Receiver (managed by new controller)
apiVersion: notification.toolkit.fluxcd.io/v1
kind: Receiver
metadata:
  name: github-receiver
  namespace: default
spec:
  type: github
  events:
    - "ping"
    - "push"
  secretRef:
    name: webhook-token
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
```

### Controller Workflow

1. **watch Receiver resources**: Monitor Receiver resources with type `github`

2. **Validate resource references**: Check if the referenced GitRepository:
   - Exists in the same namespace
   - Is managed by the extension controller (belongs to configured organization)
   - Has a valid repository URL

3. **Manage webhook secret**:
   - If `secretRef.name` exists: Reuse the existing webhook token from the secret
   - If `secretRef.name` does not exist: Generate a new random webhook token (secure random string, 64 characters)
   - Create/update Kubernetes secret with the token in the format expected by Flux Receiver

4. **Construct webhook URL**:
   - Read `webhookPath` from Receiver's `.status.webhookPath`
   - Append to configured base URL (from controller configuration)
   - Example: `https://flux.example.com` + `/hook/abc123def456` = `https://flux.example.com/hook/abc123def456`

5. **Register GitHub webhook**:
   - Use GitHub App authentication (existing mechanism)
   - Create or update webhook for the repository
   - Configure webhook to send only the events listed in `spec.events`
   - Set webhook secret to match the generated token
   - Set content type to `application/json`
   - Enable SSL verification

6. **Update status**: Record webhook registration status in the Receiver resource

7. **Handle lifecycle**:
   - Update webhook when Receiver spec changes (events, resources)
   - Delete webhook when Receiver is deleted
   - Recreate webhook if manually deleted in GitHub

## Configuration Changes

### Controller Configuration

Add webhook configuration to the controller config:

```yaml
# config.yaml
github:
  appId: 123456
  privateKeyPath: "/etc/github/private-key"
  organization: "your-org"

controller:
  excludedNamespaces:
    - "kube-system"
  watchAllNamespaces: true

webhook:
  baseURL: "https://flux.example.com"  # Base URL for webhook endpoints

tokenRefresh:
  refreshInterval: "50m"
  tokenLifetime: "60m"
```

### Helm Chart Values

```yaml
# values.yaml
webhook:
  # Base URL for Flux webhook receiver endpoints
  # This URL should be accessible from GitHub
  baseURL: ""  # Required when webhook feature is enabled
  
  # Enable webhook registration feature
  enabled: true
```

## GitHub Webhook Configuration

### Webhook Properties

When registering webhooks, the controller will configure:

- **URL**: `{baseURL}{webhookPath}`
- **Content type**: `application/json`
- **Secret**: Generated webhook token (for HMAC validation)
- **Events**: Only events specified in Receiver `spec.events`
- **Active**: `true`
- **SSL verification**: Enabled

### Event Mapping

Common Flux Receiver events map to GitHub webhook events:

| Receiver Event | GitHub Webhook Event |
|---------------|---------------------|
| `ping` | `ping` |
| `push` | `push` |
| `pull_request` | `pull_request` |
| `create` | `create` |
| `delete` | `delete` |
| `release` | `release` |

### GitHub Permissions Required

The GitHub App must have the following additional permissions:

- **Repository permissions**:
  - `Webhooks: Read & write` (for webhook management)
  - `Contents: Read` (already required)
  - `Metadata: Read` (already required)

## Webhook Token Management

### Token Generation

When a webhook token secret doesn't exist:

1. Generate a cryptographically secure random token (64 alphanumeric characters)
2. Create Kubernetes secret with the following structure:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: webhook-token
  namespace: default
  labels:
    app.kubernetes.io/managed-by: flux-extension-controller
  annotations:
    flux-extension.nrfcloud.com/managed-for: "receiver/github-receiver"
type: Opaque
data:
  token: <base64-encoded-token>
```

### Token Reuse

When the secret already exists:

1. Read the token from `data.token` (base64 decoded)
2. Use the existing token for webhook configuration
3. Update secret annotations if needed

### Token Security

- Tokens are stored in Kubernetes secrets (encrypted at rest if configured)
- Tokens are transmitted to GitHub over HTTPS
- Tokens are used for HMAC-SHA256 validation by Flux Receiver
- Each Receiver can have its own unique token

## Implementation Approach

### New Components

1. **Receiver Controller** (`controllers/receiver_controller.go`)
   - Watches Receiver resources
   - Reconciles webhook registration
   - Manages webhook lifecycle

2. **Webhook Manager** (`pkg/webhook/manager.go`)
   - Handles webhook token generation
   - Creates/updates/deletes GitHub webhooks
   - Manages webhook secrets

3. **GitHub Client Extensions** (`pkg/github/webhook.go`)
   - Extend GitHubClient interface with webhook methods:
     - `CreateWebhook(ctx, repoURL, webhookURL, secret, events)`
     - `UpdateWebhook(ctx, repoURL, webhookID, webhookURL, secret, events)`
     - `DeleteWebhook(ctx, repoURL, webhookID)`
     - `GetWebhook(ctx, repoURL, webhookURL)`

4. **Configuration Updates** (`pkg/config/config.go`)
   - Add `WebhookConfig` struct
   - Add `BaseURL` field for webhook endpoint base URL

### Integration Points

- **GitRepository Controller**: Query to check if repository is managed
- **Secret Manager**: Reuse existing secret management utilities
- **GitHub Client**: Extend with webhook operations using GitHub App authentication

### RBAC Requirements

Add new RBAC permissions:

```yaml
# +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers,verbs=get;list;watch;update;patch
# +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers/status,verbs=get;update;patch
# +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
```

## Receiver Status Updates

The controller will update the Receiver status with webhook information:

```yaml
status:
  conditions:
    - type: WebhookRegistered
      status: "True"
      reason: "WebhookCreated"
      message: "Webhook registered successfully for repository your-org/webapp"
      lastTransitionTime: "2024-01-15T10:30:00Z"
  webhookPath: "/hook/abc123def456"  # Set by Flux Receiver controller
  observedGeneration: 1
```

Additional status fields added by extension controller:

```yaml
status:
  # Added by flux-extension-controller
  webhookID: "12345678"  # GitHub webhook ID
  webhookURL: "https://flux.example.com/hook/abc123def456"  # Full webhook URL
```

## Error Handling

### Validation Errors

- **Missing GitRepository**: Log warning, requeue after 5 minutes
- **Unmanaged GitRepository**: Skip reconciliation (not an error)
- **Invalid webhook base URL**: Fail reconciliation, update status condition
- **Missing webhookPath**: Wait for Flux Receiver controller to set it, requeue after 30 seconds

### GitHub API Errors

- **Permission denied**: Update status condition, log error, requeue after 5 minutes
- **Repository not found**: Update status condition, log error, requeue after 5 minutes
- **Rate limit exceeded**: Requeue with exponential backoff (up to 1 hour)
- **Network errors**: Retry with exponential backoff

### Secret Management Errors

- **Failed to create secret**: Update status condition, log error, fail reconciliation
- **Failed to read existing secret**: Update status condition, log error, fail reconciliation

## Testing Strategy

### Unit Tests

1. **Receiver Controller Tests**:
   - Test reconciliation logic
   - Test resource validation
   - Test status updates
   - Test error handling

2. **Webhook Manager Tests**:
   - Test token generation (randomness, length, uniqueness)
   - Test secret creation/update
   - Test webhook URL construction

3. **GitHub Client Tests**:
   - Test webhook creation with mocked GitHub API
   - Test webhook updates
   - Test webhook deletion
   - Test error handling

### Integration Tests

1. **End-to-End Workflow**:
   - Create GitRepository (managed)
   - Create Receiver referencing GitRepository
   - Verify webhook secret created
   - Verify webhook registered (mocked)
   - Verify status updated

2. **Update Scenarios**:
   - Update Receiver events, verify webhook updated
   - Update Receiver resources, verify webhook recreated
   - Delete Receiver, verify webhook deleted

3. **Edge Cases**:
   - Receiver referencing unmanaged GitRepository (skip)
   - Receiver with existing secret (reuse)
   - Receiver without webhookPath (wait and retry)
   - Multiple Receivers for same GitRepository (multiple webhooks)

### Manual Testing Checklist

- [ ] Deploy controller with webhook configuration
- [ ] Create Receiver with new secret - verify webhook created
- [ ] Create Receiver with existing secret - verify secret reused
- [ ] Update Receiver events - verify webhook updated
- [ ] Delete Receiver - verify webhook deleted
- [ ] Check GitHub webhook settings match configuration
- [ ] Verify webhook deliveries work (requires Flux Receiver)

## Security Considerations

### Authentication

- GitHub App authentication used for webhook management (existing mechanism)
- Webhook tokens stored securely in Kubernetes secrets
- TLS/HTTPS required for webhook endpoints

### Authorization

- Controller only operates on receivers referencing managed repositories
- RBAC controls which namespaces controller can access
- Webhook secrets are namespace-scoped

### Validation

- Validate webhook base URL is HTTPS (recommended)
- Validate Receiver references exist and are accessible
- Validate GitHub App has required permissions

### Secret Rotation

- Webhook tokens are long-lived (no automatic rotation)
- Manual rotation: Delete secret, controller will regenerate and update webhook
- Consider implementing token rotation in future enhancement

## Migration and Rollout

### Enabling the Feature

1. **Update controller configuration**:
   ```yaml
   webhook:
     baseURL: "https://flux.example.com"
   ```

2. **Upgrade controller** with webhook feature enabled

3. **Update GitHub App permissions** to include webhook management

4. **Create Receiver resources** - webhooks will be registered automatically

### Disabling the Feature

1. Set `webhook.enabled: false` in configuration
2. Controller will stop managing webhooks
3. Existing webhooks remain (manual cleanup required)

### Backward Compatibility

- Feature is opt-in (requires configuration)
- Existing GitRepository and ConfigMap features unaffected
- No changes to existing API contracts

## Future Enhancements

1. **Multi-cluster Support**: Support webhook receivers across multiple clusters
2. **Webhook Health Monitoring**: Monitor webhook delivery success/failure rates
3. **Token Rotation**: Automatic periodic rotation of webhook tokens
4. **Event Filtering**: More granular event filtering based on branches/tags
5. **Custom Webhook Headers**: Support for custom headers in webhook requests
6. **Webhook Retry Configuration**: Configure retry behavior for failed deliveries

## References

- [Flux Notification Controller](https://fluxcd.io/flux/components/notification/)
- [Flux Receiver API](https://fluxcd.io/flux/components/notification/receiver/)
- [GitHub Webhooks Documentation](https://docs.github.com/en/webhooks)
- [GitHub Apps API - Webhooks](https://docs.github.com/en/rest/webhooks)
- [Existing GitHub Token Management](./github-token-management.md)
