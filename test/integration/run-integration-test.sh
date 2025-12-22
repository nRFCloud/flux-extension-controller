#!/usr/bin/env bash

# Integration test script for flux-extension-controller
# This script sets up a kind cluster, installs Flux, deploys the controller, and validates functionality

set -euo pipefail

# Configuration
CLUSTER_NAME="${CLUSTER_NAME:-flux-extension-test}"
FLUX_VERSION="${FLUX_VERSION:-v2.3.0}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.29.2}"
TIMEOUT="${TIMEOUT:-300}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $*"
}

# Cleanup function
cleanup() {
    local exit_code=$?
    log_info "Cleaning up..."
    
    if [ "${SKIP_CLEANUP:-false}" != "true" ]; then
        kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
    else
        log_warning "Skipping cleanup (SKIP_CLEANUP=true)"
        log_info "Cluster '${CLUSTER_NAME}' is still running"
        log_info "To access: kubectl cluster-info --context kind-${CLUSTER_NAME}"
    fi
    
    exit $exit_code
}

trap cleanup EXIT

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    local missing_tools=()
    
    if ! command -v kind &> /dev/null; then
        missing_tools+=("kind")
    fi
    
    if ! command -v kubectl &> /dev/null; then
        missing_tools+=("kubectl")
    fi
    
    if ! command -v docker &> /dev/null; then
        missing_tools+=("docker")
    fi
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_error "Please install the missing tools and try again"
        exit 1
    fi
    
    log_info "All prerequisites satisfied"
}

# Install Flux CLI if not present
install_flux_cli() {
    if command -v flux &> /dev/null; then
        log_info "Flux CLI already installed: $(flux --version)"
        return 0
    fi
    
    log_info "Installing Flux CLI ${FLUX_VERSION}..."
    
    local os
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch
    arch=$(uname -m)
    
    case $arch in
        x86_64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) log_error "Unsupported architecture: $arch"; exit 1 ;;
    esac
    
    local flux_url="https://github.com/fluxcd/flux2/releases/download/${FLUX_VERSION}/flux_${FLUX_VERSION#v}_${os}_${arch}.tar.gz"
    
    curl -sL "$flux_url" | tar xz -C /tmp
    sudo mv /tmp/flux /usr/local/bin/flux
    chmod +x /usr/local/bin/flux
    
    log_info "Flux CLI installed: $(flux --version)"
}

# Create kind cluster
create_kind_cluster() {
    log_info "Creating kind cluster '${CLUSTER_NAME}' with image ${KIND_NODE_IMAGE}..."
    
    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log_warning "Cluster '${CLUSTER_NAME}' already exists, deleting it..."
        kind delete cluster --name "${CLUSTER_NAME}"
    fi
    
    cat <<EOF | kind create cluster --name "${CLUSTER_NAME}" --image "${KIND_NODE_IMAGE}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
EOF
    
    # Wait for cluster to be ready
    log_info "Waiting for cluster to be ready..."
    kubectl wait --for=condition=ready node --all --timeout="${TIMEOUT}s"
    
    log_info "Kind cluster created successfully"
}

# Install Flux
install_flux() {
    log_info "Installing Flux components..."
    
    # Generate Flux manifests and apply them directly (bypass verification)
    flux install \
        --components=source-controller,notification-controller \
        --network-policy=false \
        --toleration-keys=node-role.kubernetes.io/control-plane \
        --export | kubectl apply -f -
    
    # Give components time to start
    log_info "Waiting for deployments to be created..."
    sleep 20
    
    # Wait for Flux deployments to be available
    log_info "Waiting for Flux components to be ready..."
    for deployment in source-controller notification-controller; do
        log_info "Waiting for $deployment..."
        kubectl -n flux-system wait deployment/$deployment \
            --for=condition=Available \
            --timeout="${TIMEOUT}s" || {
                log_error "$deployment failed to become available"
                kubectl -n flux-system describe deployment/$deployment
                kubectl -n flux-system logs deployment/$deployment --tail=50 || true
                return 1
            }
    done
    
    # Verify pods are running
    kubectl -n flux-system get pods
    
    log_info "Flux installation complete"
}

# Build and load controller image
build_and_load_image() {
    log_info "Building controller Docker image..."
    
    cd "${REPO_ROOT}"
    
    # Build the image
    local image_tag="ghcr.io/nrfcloud/flux-extension-controller:integration-test"
    docker build -t "${image_tag}" .
    
    # Load image into kind cluster
    log_info "Loading image into kind cluster..."
    kind load docker-image "${image_tag}" --name "${CLUSTER_NAME}"
    
    log_info "Controller image built and loaded"
}

# Deploy controller
deploy_controller() {
    log_info "Deploying flux-extension-controller..."
    
    # Create a test configuration
    helm upgrade --install flux-extension-controller "${REPO_ROOT}/chart" \
        --namespace flux-system \
        --create-namespace \
        --set image.repository=ghcr.io/nrfcloud/flux-extension-controller \
        --set image.tag=integration-test \
        --set image.pullPolicy=Never \
        --wait \
        --timeout="${TIMEOUT}s"
    
    # Wait for controller to be ready
    log_info "Waiting for controller to be ready..."
    kubectl -n flux-system wait --for=condition=ready pod -l app.kubernetes.io/name=flux-extension-controller --timeout="${TIMEOUT}s"
    
    log_info "Controller deployed successfully"
}

# Run validation tests
run_validation_tests() {
    log_info "Running validation tests..."
    
    # Test 1: ConfigMap synchronization
    test_configmap_sync
    
    # Test 2: Namespace watching
    test_namespace_watching
    
    log_info "All validation tests passed!"
}

# Test ConfigMap synchronization
test_configmap_sync() {
    log_info "Test 1: ConfigMap synchronization"
    
    # Create a target namespace
    kubectl create namespace test-target
    kubectl annotate namespace test-target flux-extension.nrfcloud.com/sync-target=true
    
    # Create a ConfigMap to sync
    kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: flux-system
  annotations:
    flux-extension.nrfcloud.com/sync-configmap: "true"
data:
  app.name: "test-app"
  log.level: "info"
EOF
    
    # Wait for ConfigMap to be synced
    log_info "Waiting for ConfigMap to be synced..."
    local retries=30
    local count=0
    while [ $count -lt $retries ]; do
        if kubectl get configmap test-config -n test-target &>/dev/null; then
            log_info "ConfigMap synced successfully"
            break
        fi
        count=$((count + 1))
        sleep 2
    done
    
    if [ $count -eq $retries ]; then
        log_error "ConfigMap was not synced within timeout"
        kubectl -n flux-system logs -l app.kubernetes.io/name=flux-extension-controller --tail=50
        return 1
    fi
    
    # Verify ConfigMap data
    local synced_data
    synced_data=$(kubectl get configmap test-config -n test-target -o jsonpath='{.data.app\.name}')
    if [ "$synced_data" != "test-app" ]; then
        log_error "ConfigMap data mismatch. Expected: test-app, Got: $synced_data"
        return 1
    fi
    
    # Verify sync annotation
    local sync_source
    sync_source=$(kubectl get configmap test-config -n test-target -o jsonpath='{.metadata.annotations.flux-extension\.nrfcloud\.com/sync-source}')
    if [ "$sync_source" != "flux-system/test-config" ]; then
        log_error "Sync source annotation mismatch. Expected: flux-system/test-config, Got: $sync_source"
        return 1
    fi
    
    log_info "✓ ConfigMap synchronization test passed"
}

# Test namespace watching
test_namespace_watching() {
    log_info "Test 2: Namespace watching"
    
    # Create a new namespace after ConfigMap exists
    kubectl create namespace test-new-target
    kubectl annotate namespace test-new-target flux-extension.nrfcloud.com/sync-target=true
    
    # Wait for ConfigMap to be synced to new namespace
    log_info "Waiting for ConfigMap to be synced to new namespace..."
    local retries=30
    local count=0
    while [ $count -lt $retries ]; do
        if kubectl get configmap test-config -n test-new-target &>/dev/null; then
            log_info "ConfigMap synced to new namespace successfully"
            break
        fi
        count=$((count + 1))
        sleep 2
    done
    
    if [ $count -eq $retries ]; then
        log_error "ConfigMap was not synced to new namespace within timeout"
        kubectl -n flux-system logs -l app.kubernetes.io/name=flux-extension-controller --tail=50
        return 1
    fi
    
    log_info "✓ Namespace watching test passed"
}

# Show cluster info for debugging
show_cluster_info() {
    log_info "Cluster information:"
    kubectl cluster-info --context "kind-${CLUSTER_NAME}"
    
    log_info "Controller logs:"
    kubectl -n flux-system logs -l app.kubernetes.io/name=flux-extension-controller --tail=20 || true
}

# Main execution
main() {
    log_info "Starting integration tests for flux-extension-controller"
    log_info "Cluster: ${CLUSTER_NAME}"
    log_info "Flux version: ${FLUX_VERSION}"
    
    check_prerequisites
    install_flux_cli
    create_kind_cluster
    install_flux
    build_and_load_image
    deploy_controller
    run_validation_tests
    
    log_info "Integration tests completed successfully!"
    show_cluster_info
}

main "$@"
