# Development

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

## Run Locally

```bash
# Build
go build -o bin/gopher-cni ./cmd/main

# Run daemon mode (default)
./bin/gopher-cni --mode=daemon

# Run init-validation mode
./bin/gopher-cni --mode=init-validation
```

## Integration Tests

Integration tests spin up a k3d cluster and run end-to-end scenarios. Requires `k3d`, `kubectl`, `helm`, and `docker`.

```bash
# Run integration tests (creates and tears down cluster automatically)
task go:test-integration

# Keep the cluster running after tests for debugging
task go:test-integration KEEP_CLUSTER=1
```
