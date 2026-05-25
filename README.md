# Gopher CNI

A Kubernetes CNI plugin that tunnels pod traffic through WireGuard VPN.

## Features

- **WireGuard VPN Tunneling** - Routes pod traffic through WireGuard tunnels at the CNI layer, transparent to the application
- **Two CNI Modes** - `pod-origin` (traffic exits via `eth0` as encrypted UDP) or `host-origin` (traffic bypasses the Kubernetes overlay entirely)
- **Split Tunneling** - Route specific CIDRs via the pod's original interface while everything else goes through WireGuard
- **DNS Tunneling** - Automatically configures pods to use DNS servers from the WireGuard config
- **Admission Webhooks** - Mutating webhook injects required containers; validating webhook catches misconfigured pods at admission time
- **Label-Based Opt-In** - Pods must explicitly opt in via a label; no cluster-wide interception

## Quick Start

### Prerequisites

- Kubernetes 1.19+
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
helm install gopher-cni oci://ghcr.io/bartlettc22/charts/gopher-cni \
  --namespace gopher-cni-system \
  --create-namespace
```

### Enable WireGuard Tunneling for a Pod

Create a secret containing the WireGuard configuration:

```bash
kubectl create secret generic my-wg-config \
  --from-file=wg.conf=/path/to/wg0.conf
```

Then add the label and annotation to your pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"
  annotations:
    gopher.cni/wgconf-secret: "my-wg-config"
spec:
  containers:
  - name: app
    image: myapp:latest
```

> **Note**: `hostNetwork: true` is not compatible with Gopher CNI. The validating webhook will reject such pods.

## How It Works

When a pod with `gopher.cni/enabled: "true"` is scheduled:

1. The **mutating webhook** injects a `gopher-cni-validator` init container.
2. The **CNI plugin** runs at pod startup, reads the WireGuard config from the referenced secret, creates a WireGuard interface inside the pod's network namespace, and installs the appropriate routes.
3. The **validating webhook** rejects pods with invalid annotation values or split-tunnel CIDRs that would break WireGuard connectivity.

## Configuration

All pod-level configuration is done via labels and annotations. See the [Configuration Reference](docs/CONFIGURATION.md) for the full list.

### Key Annotations

| Annotation | Description | Default |
|---|---|---|
| `gopher.cni/wgconf-secret` | Name of the Kubernetes secret containing `wg.conf` | *(required)* |
| `gopher.cni/cni-mode` | `pod-origin` or `host-origin` | `pod-origin` |
| `gopher.cni/dns-tunneled` | Tunnel DNS via WireGuard using DNS servers from the config | `true` |
| `gopher.cni/split-tunnel-cidrs` | Comma-separated CIDRs to route via the original interface | `""` |
| `gopher.cni/split-tunnel-overlap` | Set to `allow` to permit split-tunnel CIDRs that overlap (but are less specific than) WireGuard addresses or DNS servers | `""` |

### WireGuard Secret Format

The secret must contain a key named `wg.conf` with a standard WireGuard INI configuration:

```ini
[Interface]
PrivateKey = <private key>
Address = 10.2.0.2/32
DNS = 10.2.0.1

[Peer]
PublicKey = <public key>
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.com:51820
```

### Split Tunneling

Split tunneling lets specific CIDRs bypass the WireGuard tunnel:

```yaml
annotations:
  gopher.cni/wgconf-secret: "my-wg-config"
  gopher.cni/split-tunnel-cidrs: "10.96.0.0/12,10.244.0.0/16"
```

The CNI plugin automatically installs protected routes via the WireGuard interface for all WireGuard addresses and DNS servers, so split-tunnel CIDRs cannot accidentally capture that traffic. The validating webhook enforces this: split-tunnel CIDRs that are the same or more specific than a protected route are always rejected; less-specific overlaps are rejected by default but can be explicitly permitted with `gopher.cni/split-tunnel-overlap: "allow"`.

### Daemon Flags

```bash
--port=8443                             # Webhook server port
--tls-cert=/etc/webhook/certs/tls.crt  # TLS certificate path
--tls-key=/etc/webhook/certs/tls.key   # TLS private key path
--image=gopher-cni:latest               # Image for injected containers
--cni-bin-path=/opt/cni/bin            # Host path for CNI binaries
--cni-config-path=/etc/cni/net.d       # Host path for CNI configuration
```

All flags can also be set via environment variables (`WEBHOOK_PORT`, `WEBHOOK_IMAGE`, `TLS_CERT_PATH`, `TLS_KEY_PATH`, `CNI_BIN_PATH`, `CNI_CONFIG_PATH`).

## Troubleshooting

Common issues:

- **Pod rejected at admission** — check the rejection message; it will identify the specific annotation that failed validation
- **WireGuard tunnel not established** — verify the secret exists in the pod's namespace and the `wg.conf` is valid
- **DNS not resolving** — confirm the WireGuard config includes a `DNS =` line, or set `gopher.cni/dns-tunneled: "false"`
- **Certificate not ready** — check cert-manager logs and `kubectl describe certificate -n gopher-cni-system`

## Documentation

- [Configuration Reference](docs/CONFIGURATION.md) — all labels, annotations, and WireGuard secret format
- [Helm Chart](chart/gopher-cni/README.md) — Helm values and installation options
- [Developer Guide](docs/DEV.md) — building, testing, and running locally
- [Changelog](CHANGELOG.md)

## License

MIT License — see [LICENSE](LICENSE) for details.
