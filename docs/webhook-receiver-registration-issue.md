# Feature Request: GitHub Webhook Registration for Flux Receivers

## Summary

Automatically register GitHub webhooks for Flux Receiver resources that reference GitRepositories managed by the flux-extension-controller. This eliminates manual webhook configuration and ensures consistent webhook setup across multiple receivers.

## Motivation

Currently, when using Flux Receivers to trigger reconciliation via webhooks:
1. Users must manually create webhooks in GitHub repository settings
2. Users must manually generate and configure webhook secrets
3. Webhook configuration can become inconsistent across repositories
4. Changes to receiver configuration require manual webhook updates

This feature automates the entire webhook lifecycle, improving user experience and reducing configuration errors.

## Proposed Solution

Extend the flux-extension-controller with a new Receiver controller that:

### Core Functionality
- **Watches Flux Receiver resources** of type `github`
- **Validates resource references**: Only acts on Receivers referencing GitRepositories already managed by the controller (same organization)
- **Manages webhook secrets**: 
  - Generates secure random webhook tokens if secret doesn't exist
  - Reuses existing tokens if secret already exists
- **Registers GitHub webhooks**:
  - Uses the Receiver's `.status.webhookPath` 
  - Appends to configured webhook base URL
  - Configures webhook to send only events specified in Receiver spec
  - Uses GitHub App authentication (existing mechanism)
- **Handles lifecycle**: Updates webhooks on changes, deletes webhooks on resource deletion

### Configuration

Add webhook configuration to controller:

```yaml
# config.yaml
webhook:
  baseURL: "https://flux.example.com"  # Base URL for webhook endpoints
```

Helm chart values:
```yaml
webhook:
  baseURL: ""  # Required when webhook feature is enabled
  enabled: true
```

### Example Usage

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
- Controller generates webhook token in secret `webhook-token` (if needed)
- Controller registers webhook: `https://flux.example.com/hook/abc123` 
- Controller configures webhook to send `ping` and `push` events
- Webhook is automatically updated if Receiver spec changes
- Webhook is automatically deleted if Receiver is deleted

## Implementation Details

Full specification available at: [docs/webhook-receiver-registration.md](./webhook-receiver-registration.md)

### Components to Add

1. **Receiver Controller** (`controllers/receiver_controller.go`)
   - Watches and reconciles Receiver resources
   - Manages webhook lifecycle
   - Updates Receiver status

2. **Webhook Manager** (`pkg/webhook/manager.go`)
   - Token generation (cryptographically secure, 64 characters)
   - Secret management (create/update webhook secrets)
   - Webhook URL construction

3. **GitHub Client Extensions** (`pkg/github/webhook.go`)
   - `CreateWebhook()`
   - `UpdateWebhook()`
   - `DeleteWebhook()`
   - `GetWebhook()`

4. **Configuration Updates** (`pkg/config/config.go`)
   - Add `WebhookConfig` struct with `BaseURL` field

### GitHub App Permissions

Requires additional permission:
- **Repository permissions**: `Webhooks: Read & write`

### RBAC

New permissions required:
```yaml
# +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers,verbs=get;list;watch;update;patch
# +kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers/status,verbs=get;update;patch
```

## Benefits

✅ **Zero manual configuration**: Webhooks created automatically  
✅ **Consistent configuration**: All webhooks configured the same way  
✅ **Automatic updates**: Webhook config updated when Receiver changes  
✅ **Secure token management**: Random tokens generated and stored securely  
✅ **Selective operation**: Only manages repos already controlled by extension controller  
✅ **Clean lifecycle**: Webhooks deleted automatically with Receivers  

## Testing Strategy

### Unit Tests
- Token generation (randomness, uniqueness)
- Secret creation/update logic
- Webhook URL construction
- GitHub API interactions (mocked)
- Error handling scenarios

### Integration Tests
- End-to-end workflow (GitRepository → Receiver → Webhook)
- Update scenarios (events, resources)
- Delete scenarios (cleanup)
- Edge cases (unmanaged repos, missing webhookPath, existing secrets)

### Manual Testing
- Deploy with webhook configuration
- Create Receivers with new/existing secrets
- Verify webhooks in GitHub UI
- Test webhook deliveries
- Verify cleanup on deletion

## Security Considerations

✅ **Authentication**: GitHub App authentication (existing mechanism)  
✅ **Authorization**: Only acts on managed repositories  
✅ **Secrets**: Tokens stored in Kubernetes secrets  
✅ **Transport**: HTTPS required for webhook endpoints  
✅ **Validation**: Validates all resource references and URLs  

## Migration and Rollout

1. **Update GitHub App** to include webhook management permission
2. **Update controller configuration** with webhook base URL
3. **Deploy updated controller** 
4. **Create Receiver resources** - webhooks automatically registered

Feature is **opt-in** and **backward compatible**:
- Requires webhook configuration to activate
- No impact on existing GitRepository or ConfigMap features
- Existing receivers without controller management are unaffected

## Dependencies

- `github.com/google/go-github/v76` (already included)
- `notification.toolkit.fluxcd.io/v1` API (need to add to go.mod)

## Acceptance Criteria

- [ ] Receiver controller watches Receiver resources
- [ ] Only acts on Receivers referencing managed GitRepositories
- [ ] Generates secure webhook tokens when secret doesn't exist
- [ ] Reuses tokens from existing secrets
- [ ] Constructs webhook URL from baseURL + webhookPath
- [ ] Creates webhooks in GitHub with correct configuration
- [ ] Updates webhooks when Receiver spec changes
- [ ] Deletes webhooks when Receiver is deleted
- [ ] Updates Receiver status with webhook information
- [ ] Comprehensive test coverage (unit + integration)
- [ ] Documentation updated
- [ ] Helm chart updated with webhook configuration

## Related Documentation

- [Full Feature Specification](./webhook-receiver-registration.md)
- [Existing GitHub Token Management](./github-token-management.md)
- [Flux Receiver Documentation](https://fluxcd.io/flux/components/notification/receiver/)

## Labels

- `enhancement`
- `feature-request` 
- `needs-triage`

## Additional Context

This feature complements the existing GitHub App token management by extending automation to webhook configuration. It follows the same patterns and conventions used in the current GitRepository controller implementation.
