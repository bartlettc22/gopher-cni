# Gopher CNI

A Kubernetes CNI plugin that tunnels pod traffic through WireGuard VPN with automatic network validation.

## Features

- **WireGuard VPN Tunneling** - Routes all pod traffic through WireGuard tunnels using CNI
- **Container Network Interface** - CNI plugin for seamless Kubernetes pod networking integration
- **Automatic Network Validation** - Validates WireGuard tunnel and network setup before pod starts
- **Admission Webhook** - Automatic injection of validation containers via webhook
- **Multi-Mode Operation** - Supports daemon and init-validation modes
- **Label-Based Control** - Simple opt-in via pod labels
- **Production Ready** - High availability, graceful shutdown, health checks, cert-manager integration

## Quick Start

### Prerequisites
- Kubernetes cluster (1.19+)
- kubectl configured
- Helm 3.0+
- [cert-manager](https://cert-manager.io/) installed

### Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set installCRDs=true
```

### Deploy Gopher CNI

```bash
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni-system \
  --create-namespace
```

### Enable WireGuard Tunneling for Pods

Add the label to your pods to enable automatic validation:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"             # Enable WireGuard tunnel validation
spec:
  containers:
  - name: app
    image: myapp:latest
```

> **⚠️ Warning**: Gopher CNI is not compatible with pods using `hostNetwork: true`. The admission webhook will reject pods that attempt to use both gopher-cni injection and host networking.

## Components

### CNI Plugin
The core component that provides WireGuard VPN tunneling:
- **Network Integration** - Integrates with existing Kubernetes CNI to add WireGuard capabilities
- **Traffic Routing** - Routes pod traffic through WireGuard VPN tunnels
- **Configuration Management** - Manages WireGuard configuration on host nodes

### Admission Webhook
Provides automatic injection capabilities:
- **Mutating Webhook** - Automatically injects validation init containers
- **Validating Webhook** - Validates pod configuration and label compatibility
- **TLS Certificates** - Managed by cert-manager with automatic renewal
- **Health Checks** - `/health` and `/ready` endpoints

### Injected Resources

**Init Container** - Validates WireGuard tunnel and CNI configuration:
```yaml
name: gopher-cni-validator
image: gopher-cni:latest
command:
- /validator
```

## Configuration

### Command-Line Flags

```bash
# Operation mode
--mode=daemon                         # daemon or init-validation (default: daemon)

# Daemon mode settings
--port=8443                           # Webhook server port (daemon mode)
--webhook-tls-disable                 # Disable TLS for webhook server (testing only, daemon mode)
--tls-cert=/etc/webhook/certs/tls.crt # TLS certificate path (daemon mode, required if TLS enabled)
--tls-key=/etc/webhook/certs/tls.key  # TLS private key path (daemon mode, required if TLS enabled)
--image=gopher-cni:latest             # Container image for injected containers (daemon mode)

# CNI settings (daemon and init-validation modes)
--cni-bin-path=/opt/cni/bin           # Host path for CNI binaries
--cni-config-path=/etc/cni/net.d      # Host path for CNI configuration
```

### Environment Variables

All flags can also be set via environment variables:
- `GOPHER_CNI_MODE`
- `WEBHOOK_PORT`
- `WEBHOOK_TLS_DISABLE`
- `WEBHOOK_IMAGE`
- `CNI_BIN_PATH`
- `CNI_CONFIG_PATH`
- `TLS_CERT_PATH`
- `TLS_KEY_PATH`

## Troubleshooting

### Check Status

```bash
# Check Helm release
helm status gopher-cni -n gopher-cni-system

# Check certificates (if using webhook)
kubectl get certificate -n gopher-cni-system
kubectl describe certificate -n gopher-cni-system
```

### Common Issues

1. **WireGuard tunnel not working**: Verify CNI plugin is installed on nodes and WireGuard configuration is correct
2. **Pods not receiving validation containers**: Verify label `gopher.cni/enabled: "true"` is set
3. **Certificate not ready**: Check cert-manager logs and certificate status
4. **Webhook timeout**: Verify webhook pods are running and healthy
5. **Helm installation fails**: Ensure cert-manager is installed and ready

## Documentation

- [Helm Chart Documentation](chart/gopher-cni/README.md) - Helm chart installation and configuration
- [Configuration Reference](docs/CONFIGURATION.md) - Detailed configuration options
- [Developer Guide](docs/DEV.md) - Building, testing, and running locally

## License

MIT License - See LICENSE file for details
