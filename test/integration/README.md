# Integration Tests

This directory contains integration tests for the flux-extension-controller that validate the controller's functionality in a real Kubernetes environment.

## Overview

The integration tests create a complete test environment using:
- **Kind (Kubernetes in Docker)** - Creates a local Kubernetes cluster
- **Flux CD** - Installs Flux source-controller and notification-controller
- **flux-extension-controller** - Builds and deploys the controller from source

## Prerequisites

The following tools must be installed on your system:

- [Docker](https://docs.docker.com/get-docker/) - Container runtime
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) - Kubernetes in Docker
- [kubectl](https://kubernetes.io/docs/tasks/tools/) - Kubernetes CLI
- [Helm](https://helm.sh/docs/intro/install/) - Kubernetes package manager

## Running Integration Tests

### Quick Start

Run the integration tests with default settings:

```bash
./test/integration/run-integration-test.sh
```

### Configuration Options

The test script supports several environment variables for customization:

```bash
# Use a custom cluster name
CLUSTER_NAME=my-test-cluster ./test/integration/run-integration-test.sh

# Use a specific Flux version
FLUX_VERSION=v2.3.0 ./test/integration/run-integration-test.sh

# Increase timeout for slower systems
TIMEOUT=600 ./test/integration/run-integration-test.sh

# Keep the cluster running after tests (for debugging)
SKIP_CLEANUP=true ./test/integration/run-integration-test.sh
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CLUSTER_NAME` | Name of the kind cluster to create | `flux-extension-test` |
| `FLUX_VERSION` | Version of Flux CLI to install | `v2.4.0` |
| `TIMEOUT` | Timeout in seconds for Kubernetes operations | `300` |
| `SKIP_CLEANUP` | Keep cluster running after tests | `false` |

## What Gets Tested

The integration tests validate:

1. **ConfigMap Synchronization**
   - Creates a ConfigMap in `flux-system` namespace with sync annotation
   - Creates a target namespace with sync-target annotation
   - Verifies ConfigMap is synchronized to target namespace
   - Verifies ConfigMap data is correctly copied
   - Verifies sync metadata annotations are correct

2. **Namespace Watching**
   - Creates a new namespace after ConfigMaps exist
   - Verifies existing ConfigMaps are synchronized to the new namespace
   - Tests the namespace controller's ability to handle new target namespaces

## Test Workflow

The integration test follows this workflow:

```
1. Check Prerequisites
   ↓
2. Install Flux CLI (if not present)
   ↓
3. Create Kind Cluster
   ↓
4. Install Flux Components (source-controller, notification-controller)
   ↓
5. Build Controller Docker Image
   ↓
6. Load Image into Kind Cluster
   ↓
7. Deploy Controller via Helm
   ↓
8. Run Validation Tests
   ↓
9. Cleanup (delete cluster)
```

## Debugging Failed Tests

If tests fail, the script will output relevant logs. You can also keep the cluster running for manual inspection:

```bash
# Run tests and keep cluster
SKIP_CLEANUP=true ./test/integration/run-integration-test.sh

# Access the cluster
kubectl cluster-info --context kind-flux-extension-test

# Check controller logs
kubectl -n flux-system logs -l app.kubernetes.io/name=flux-extension-controller

# List all resources
kubectl get all -A

# Delete the cluster when done
kind delete cluster --name flux-extension-test
```

## CI Integration

These integration tests are designed to run in CI environments. See `.github/workflows/ci.yaml` for the CI configuration.

The CI workflow:
1. Sets up the required tools (kind, kubectl, Helm, Flux)
2. Runs the integration test script
3. Automatically cleans up resources

## Adding New Tests

To add new integration test cases:

1. Add a new test function in `run-integration-test.sh`:
   ```bash
   test_my_new_feature() {
       log_info "Test: My new feature"
       
       # Test setup
       # ...
       
       # Verification
       # ...
       
       log_info "✓ My new feature test passed"
   }
   ```

2. Call the test function from `run_validation_tests()`:
   ```bash
   run_validation_tests() {
       log_info "Running validation tests..."
       test_configmap_sync
       test_namespace_watching
       test_my_new_feature  # Add here
       log_info "All validation tests passed!"
   }
   ```

## Troubleshooting

### Docker Permission Errors

If you see permission errors when running Docker commands:

```bash
# Add your user to the docker group
sudo usermod -aG docker $USER

# Log out and back in for changes to take effect
```

### Kind Cluster Creation Fails

If the kind cluster fails to create:

```bash
# Check Docker is running
docker ps

# Clean up any existing clusters
kind delete cluster --name flux-extension-test

# Try creating the cluster manually
kind create cluster --name flux-extension-test
```

### Flux Installation Fails

If Flux fails to install:

```bash
# Check Flux CLI version
flux --version

# Try manual installation
flux install --components=source-controller,notification-controller

# Check installation status
flux check
```

### Controller Not Starting

If the controller pod doesn't start:

```bash
# Check pod status
kubectl -n flux-system get pods

# Check pod events
kubectl -n flux-system describe pod -l app.kubernetes.io/name=flux-extension-controller

# Check logs
kubectl -n flux-system logs -l app.kubernetes.io/name=flux-extension-controller
```

## Local Development

For local development and testing:

```bash
# Build and run tests
make build
./test/integration/run-integration-test.sh

# Or run with debugging enabled
SKIP_CLEANUP=true ./test/integration/run-integration-test.sh

# Then interact with the cluster
export KUBECONFIG="$(kind get kubeconfig-path --name=flux-extension-test)"
kubectl get pods -A
```

## Resources

- [Kind Documentation](https://kind.sigs.k8s.io/)
- [Flux CD Documentation](https://fluxcd.io/docs/)
- [Kubernetes Documentation](https://kubernetes.io/docs/)
