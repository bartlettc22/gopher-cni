# Development

## Architecture

Gopher CNI is a Container Network Interface (CNI) plugin that routes Kubernetes pod traffic through WireGuard VPN tunnels. The CNI plugin integrates with the existing primary CNI to add WireGuard tunneling capabilities to pods.

The system operates in two modes:

### 1. Daemon Mode (Default)
Runs as a DaemonSet and provides two main functions:
- **CNI Plugin Installation**: Installs and maintains the CNI plugin binary on each node at `/opt/cni/bin` and manages the CNI configuration
- **Admission Webhook Server**: Runs the mutating/validating webhook server for automatic injection of validation containers
- Handles cleanup on shutdown (removes plugin binary and configuration)
- Continuously monitors and maintains the CNI plugin installation across the cluster

### 2. Init-Validation Mode
Runs as an init container on gopher-CNI enabled pods:
- Validates that the CNI plugin binary and configuration are properly installed by:
  - Validating the Gopher CNI interface exists
  - Validating the interface is up
  - Validating the interface has an IP address assigned
- Exits with non-zero code if validation fails, preventing the pod from starting

## Build

```bash
# Build Docker image
task docker:build

# Run tests
task go:test

# Format code
task go:fmt

# Lint code
task go:lint

# Lint Helm chart
task helm:lint
```

## Integration Tests

Integration tests spin up a k3d cluster and run end-to-end scenarios. Requires `k3d`, `kubectl`, `helm`, and `docker`.

```bash
# Run integration tests (creates and tears down cluster automatically)
task go:test-integration

# Keep the cluster running after tests for debugging
task go:test-integration KEEP_CLUSTER=1
```
