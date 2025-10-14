# Flux Extension Controller

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

The Flux Extension Controller is a Kubernetes controller built with Go that extends Flux CD functionality. It provides GitHub App token management for private repositories and ConfigMap synchronization across namespaces. The project is built using controller-runtime and extensively tested.

## Working Effectively

### Bootstrap and Build
Run these commands in order - all have been validated to work:

- `go version` -- should show Go 1.25+
- `make deps` -- downloads dependencies (takes ~30 seconds, sets timeout to 60 seconds)
- `make build` -- builds the manager binary (takes ~1 second, sets timeout to 30 seconds)
- `go fmt ./...` -- formats code
- `go vet ./...` -- runs static analysis
- `go test ./...` -- runs tests without coverage (avoids covdata tool error; do not use coverage flags)

### Helm Chart Validation
- `make helm-lint` -- lints the Helm chart (takes <1 second, sets timeout to 30 seconds)
- `make helm-template` -- validates chart templating (takes <1 second, sets timeout to 30 seconds)

**DO NOT run Docker commands** - `make docker-build` fails due to network restrictions in the sandbox environment.

### Running the Controller
- `./bin/manager --help` -- shows available command line options
- Controller requires Kubernetes cluster access and configuration file
- Sample config available in `config.yaml.example`

## Validation Scenarios

**ALWAYS test these scenarios after making changes:**

### 1. Basic Build and Test Validation
```bash
make deps && make build && go fmt ./... && go vet ./... && go test ./...
```

### 2. Helm Chart Validation
```bash
make helm-lint && make helm-template > /dev/null
```

### 3. Configuration Validation
Test with the example config:
```bash
cp config.yaml.example /tmp/test-config.yaml
./bin/manager -config /tmp/test-config.yaml --help
```

### 4. Code Quality Checks
Always run before committing:
- `go fmt ./...` -- MUST show no output (no changes)
- `go vet ./...` -- MUST show no errors
- `make helm-lint` -- MUST show "1 chart(s) linted, 0 chart(s) failed"

## Navigation and Key Locations

### Project Structure
```
./   # (repo root)
├── cmd/manager/main.go          # Main application entry point
├── controllers/                 # Kubernetes controllers (7 Go files)
│   ├── configmap_controller.go     # ConfigMap synchronization logic
│   ├── gitrepository_controller.go # GitHub token management logic
│   └── namespace_controller.go     # Namespace watching logic
├── pkg/                        # Core packages (18 Go files in pkg/)
│   ├── config/                     # Configuration management
│   ├── github/                     # GitHub API integration
│   ├── kubernetes/                 # Kubernetes client utilities
│   └── token/                      # Token refresh management
├── chart/                      # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml                 # Default configuration values
│   └── templates/                  # Kubernetes manifests
├── docs/                       # Documentation
│   ├── github-token-management.md  # GitHub App setup guide
│   └── configmap-sync.md          # ConfigMap sync guide
├── Makefile                    # Build automation
├── go.mod                      # Go dependencies (uses Go 1.25)
└── config.yaml.example         # Sample configuration
```

### Frequently Modified Files
When working on features, these are the most commonly edited files:
- `controllers/*_controller.go` -- Controller logic
- `pkg/config/config.go` -- Configuration structure
- `pkg/github/*.go` -- GitHub API integration
- `pkg/kubernetes/*.go` -- Kubernetes operations
- `chart/values.yaml` -- Helm chart defaults

### Test Files
All test files follow `*_test.go` pattern:
- 8 test files covering all packages
- Tests use testify/assert and testify/require
- Integration tests in `controllers/*_integration_test.go`
- Mock implementations for external services

## Common Commands Reference

### Development Commands (all validated)
```bash
# Check project status
ls -la                          # Shows 17 top-level items
find . -name "*.go" | wc -l     # Shows 18 Go source files
find . -name "*_test.go" | wc -l # Shows 8 test files

# Quick validation
make deps                       # Downloads Go modules (~30s)
make build                      # Builds binary (~1s) 
go test ./...                   # Runs tests (~2s)
make helm-lint                  # Validates Helm chart (<1s)

# Binary usage
./bin/manager --help            # Shows CLI options
./bin/manager -config /path/to/config.yaml  # Runs controller
```

### File Operations
```bash
# Key configuration files
cat config.yaml.example         # Sample controller configuration
cat chart/values.yaml          # Helm chart defaults
cat Makefile                   # All available make targets

# Documentation
cat README.md                   # Installation and usage guide  
cat docs/github-token-management.md  # GitHub App setup
cat docs/configmap-sync.md     # ConfigMap synchronization guide
```

## Critical Requirements

### Timing and Timeouts
- Build: ~1 second (use 30 second timeout)
- Tests: ~2 seconds (use 60 second timeout)  
- Dependencies: ~30 seconds (use 60 second timeout)
- Helm operations: <1 second (use 30 second timeout)

### Error Handling
- **Coverage profile error**: Use `go test ./...` instead of `make test`
- **Docker build error**: Skip Docker commands - network restrictions prevent package installation
- **Kubeconfig error**: Expected when running controller outside cluster

### Required Tools
- Go 1.25+ (installed and working)
- Helm 3.x (installed and working)
- Make (for build automation)

**DO NOT attempt to install additional tools** - all required tools are already available and working.

## Features Overview

### GitHub App Token Management
- Automatically generates GitHub App installation tokens
- Refreshes tokens before expiration (default: every 50 minutes)
- Creates/manages Kubernetes secrets for GitRepository resources
- Supports organization-scoped access

### ConfigMap Synchronization  
- Syncs ConfigMaps from `flux-system` namespace to target namespaces
- Uses annotations to mark ConfigMaps for sync and target namespaces
- Maintains data consistency across environments
- Supports both string data and binary data

### Controller Features
- Built with controller-runtime framework
- Supports leader election for high availability
- Comprehensive logging and metrics
- Extensive test coverage (>70% across all packages)