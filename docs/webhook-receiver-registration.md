# Webhook Receiver Registration

This document describes the automated GitHub webhook registration feature for Flux Receiver resources.

## Overview

The flux-extension-controller automatically registers GitHub webhooks for Flux Receiver resources that reference GitRepositories managed by the controller. This eliminates manual webhook configuration and ensures consistent webhook setup across multiple receivers.

## Features

- **Automatic webhook creation**: Webhooks are automatically created in GitHub when Receiver resources are created
- **Secure token management**: Webhook tokens are automatically generated and stored securely in Kubernetes secrets
- **Token reuse**: Existing webhook tokens in secrets are reused when available
- **Automatic updates**: Webhooks are updated when Receiver configuration changes
- **Automatic cleanup**: Webhooks are deleted when Receiver resources are deleted
- **Organization-scoped**: Only manages webhooks for repositories in the configured GitHub organization

## Prerequisites

### GitHub App Permissions

The GitHub App must have the following repository permissions:

- **Webhooks**: Read & write

### Controller Configuration

The controller must be configured with a webhook base URL:

```yaml
# config.yaml
webhook:
  baseURL: "https://flux.example.com"  # Base URL where Flux notification controller is accessible
```

Or via Helm values:

```yaml
# values.yaml
webhook:
  baseURL: "https://flux.example.com"
```

Or via environment variable:

```bash
export WEBHOOK_BASE_URL="https://flux.example.com"
```

## Usage

### Basic Example

```yaml
---
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
    name: webhook-token  # Auto-generated if doesn't exist
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
```

After creating the Receiver:
1. Controller generates webhook token in secret `webhook-token` (if it doesn't exist)
2. Controller waits for notification controller to populate `.status.webhookPath`
3. Controller constructs full webhook URL: `https://flux.example.com/hook/abc123`
4. Controller registers webhook in GitHub repository
5. Controller stores webhook ID in Receiver annotations for future updates/cleanup

### Configuration Options

#### Events

Specify which GitHub events trigger the webhook:

```yaml
spec:
  events:
    - "ping"
    - "push"
    - "pull_request"
```

Default: `["push"]`

#### Webhook Secret

You can either:
- Let the controller generate a new secret automatically
- Provide an existing secret (must be managed by this controller)

```yaml
spec:
  secretRef:
    name: my-webhook-token  # Will be created if doesn't exist
```

#### Multiple Repositories

A single Receiver can trigger reconciliation for multiple GitRepositories:

```yaml
spec:
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: infrastructure
```

Note: The controller will only manage webhooks for the first GitRepository in the list that belongs to the configured organization.

## Behavior

### Webhook Lifecycle

1. **Creation**: When a Receiver is created:
   - Controller validates the referenced GitRepository
   - Controller ensures webhook secret exists (creates if needed)
   - Controller waits for notification controller to populate webhookPath
   - Controller creates webhook in GitHub with configured events
   - Controller stores webhook ID in Receiver annotations

2. **Updates**: When a Receiver is updated:
   - Controller detects changes to events or webhook path
   - Controller updates webhook configuration in GitHub
   - Webhook ID remains the same

3. **Deletion**: When a Receiver is deleted:
   - Controller deletes webhook from GitHub
   - Kubernetes garbage collection handles secret cleanup (if owned by Receiver)

### Filtering Criteria

The controller only acts on Receivers that meet ALL these conditions:

- ✅ Receiver type is `github`
- ✅ Receiver is not suspended (`spec.suspend: false`)
- ✅ Referenced GitRepository exists
- ✅ GitRepository URL belongs to configured GitHub organization
- ✅ Webhook base URL is configured
- ✅ Receiver has `.status.webhookPath` populated

### Status Updates

The controller updates the Receiver status with a "Ready" condition:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: WebhookCreated
      message: "GitHub webhook created: https://flux.example.com/hook/abc123 (ID: 123456789)"
```

## Security Considerations

### Webhook Tokens

- Tokens are 64-character cryptographically secure random hex strings
- Tokens are stored in Kubernetes secrets
- Tokens are sent securely to GitHub over HTTPS
- Existing tokens are reused when secrets already exist

### Secret Ownership

Secrets created by the controller are:
- Labeled with `app.kubernetes.io/managed-by: flux-extension-controller`
- Owned by the Receiver resource (deleted when Receiver is deleted)
- Only reused if they have the correct management label

### GitHub Authentication

- Uses existing GitHub App authentication mechanism
- Requires GitHub App installation on the repository
- Webhook operations use short-lived installation tokens

## Troubleshooting

### Receiver not creating webhook

Check these conditions:

1. **Webhook feature configured?**
   ```bash
   kubectl logs -n flux-extension-controller deployment/flux-extension-controller | grep "Webhook feature not configured"
   ```

2. **Receiver type is github?**
   ```bash
   kubectl get receiver github-receiver -o jsonpath='{.spec.type}'
   ```

3. **GitRepository exists and belongs to org?**
   ```bash
   kubectl get gitrepository webapp -o jsonpath='{.spec.url}'
   ```

4. **WebhookPath populated?**
   ```bash
   kubectl get receiver github-receiver -o jsonpath='{.status.webhookPath}'
   ```
   If empty, wait for notification controller to populate it.

5. **Check controller logs:**
   ```bash
   kubectl logs -n flux-extension-controller deployment/flux-extension-controller -f
   ```

### Webhook not triggering reconciliation

This is likely a notification controller issue, not related to webhook registration. Check:

1. Notification controller logs
2. Webhook secret is correct
3. Webhook deliveries in GitHub UI (Settings → Webhooks → Recent Deliveries)

### Webhook ID not stored

The webhook ID is stored in Receiver annotations:

```bash
kubectl get receiver github-receiver -o jsonpath='{.metadata.annotations}'
```

Expected annotations:
- `flux-extension-controller.nrfcloud.com/webhook-id`: GitHub webhook ID
- `flux-extension-controller.nrfcloud.com/repository-url`: Repository URL

If missing, webhook may not have been created successfully. Check controller logs.

## Examples

### Minimal Configuration

```yaml
apiVersion: notification.toolkit.fluxcd.io/v1
kind: Receiver
metadata:
  name: minimal
  namespace: default
spec:
  type: github
  secretRef:
    name: webhook-token
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
```

### Multiple Events

```yaml
apiVersion: notification.toolkit.fluxcd.io/v1
kind: Receiver
metadata:
  name: multi-event
  namespace: default
spec:
  type: github
  events:
    - "ping"
    - "push"
    - "pull_request"
    - "release"
  secretRef:
    name: webhook-token
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
```

### Cross-Namespace References

```yaml
apiVersion: notification.toolkit.fluxcd.io/v1
kind: Receiver
metadata:
  name: cross-ns
  namespace: flux-system
spec:
  type: github
  secretRef:
    name: webhook-token
  resources:
    - apiVersion: source.toolkit.fluxcd.io/v1
      kind: GitRepository
      name: webapp
      namespace: production  # Different namespace
```

## Limitations

1. Only works with GitHub repositories (not GitLab, Bitbucket, etc.)
2. Only manages repositories in the configured GitHub organization
3. Requires notification controller to be accessible via HTTP/HTTPS
4. Webhook base URL must be publicly accessible from GitHub
5. One webhook per Receiver (not per GitRepository reference)

## Related Documentation

- [GitHub Token Management](github-token-management.md)
- [Flux Receiver Documentation](https://fluxcd.io/flux/components/notification/receiver/)
- [GitHub Webhooks Documentation](https://docs.github.com/en/webhooks)
