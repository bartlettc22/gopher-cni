# Gopher CNI Helm Chart

A Helm chart for deploying Gopher CNI to Kubernetes. Gopher CNI is a CNI plugin that tunnels pod traffic through WireGuard VPN with automatic network validation. This chart deploys a DaemonSet that installs the CNI plugin on every node and runs the admission webhook, which automatically injects a WireGuard validator init container, configures pod DNS through the tunnel, and optionally sets up split-DNS via a CoreDNS sidecar.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- [cert-manager](https://cert-manager.io/) installed in the cluster

## Namespace PodSecurity requirement

This chart deploys a DaemonSet that mounts host paths (`/opt/cni/bin`, `/etc/cni/net.d`, `/var/run/gopher-cni`), which is required for CNI plugin installation. Clusters with [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) enforced at `baseline` or `restricted` will block these pods.

Before installing, label the target namespace to allow privileged pods:

```bash
kubectl create namespace gopher-cni
kubectl label namespace gopher-cni \
  pod-security.kubernetes.io/enforce=privileged \
  pod-security.kubernetes.io/enforce-version=latest
```

## Installing cert-manager

If you don't have cert-manager installed:

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set installCRDs=true
```

## Installing the Chart

```bash
helm install gopher-cni oci://ghcr.io/bartlettc22/charts/gopher-cni \
  --namespace gopher-cni \
  --create-namespace
```

Or install from a local chart:

```bash
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni \
  --create-namespace
```

## Uninstalling the Chart

```bash
helm uninstall gopher-cni --namespace gopher-cni
```

## Configuration

The following table lists the configurable parameters of the Gopher CNI chart and their default values.

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `ghcr.io/bartlettc22/gopher-cni` |
| `image.tag` | Image tag | `""` (uses chart appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override fully-qualified app name | `""` |

### Logging

| Parameter | Description | Default |
|-----------|-------------|---------|
| `logLevel` | Log level (`debug`, `info`, `warn`, `error`) | `info` |
| `logFormat` | Log format (`json`, `text`) | `json` |

### CNI

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cni.host.cniNetDir` | Host path for CNI config files | `/etc/cni/net.d` |
| `cni.host.binPath` | Host path for CNI binaries | `/opt/cni/bin` |
| `cni.host.udsSocketDir` | Host path for the UDS log socket directory | `/var/run/gopher-cni` |
| `cni.containerHostPathPrefix` | Container mount prefix for host paths | `/host` |

### Webhook

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.port` | Webhook server port | `8443` |
| `webhook.injectedImage` | Image used for injected init/sidecar containers | `""` (uses daemonset image) |
| `webhook.tls.certPath` | Path to TLS certificate inside the container | `/etc/webhook/certs/tls.crt` |
| `webhook.tls.keyPath` | Path to TLS private key inside the container | `/etc/webhook/certs/tls.key` |
| `webhook.failurePolicy` | Webhook failure policy (`Fail` or `Ignore`) | `Fail` |
| `webhook.timeoutSeconds` | Webhook timeout in seconds | `10` |

### Certificate (cert-manager)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `certificate.enabled` | Create a cert-manager Certificate resource | `true` |
| `certificate.duration` | Certificate validity duration | `2160h` (90 days) |
| `certificate.renewBefore` | Renew certificate before expiry | `360h` (15 days) |
| `certificate.issuer.create` | Create a self-signed Issuer and CA | `true` |
| `certificate.issuer.name` | Name of an existing Issuer to use (requires `issuer.create: false`) | `""` |
| `certificate.issuer.kind` | Issuer kind (`Issuer` or `ClusterIssuer`) | `Issuer` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.annotations` | Annotations for the service account | `{}` |
| `serviceAccount.name` | Service account name (auto-generated if empty) | `""` |

### Pod

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAnnotations` | Annotations added to each pod | `{}` |
| `podLabels` | Labels added to each pod | `{}` |
| `podSecurityContext` | Pod-level security context | `{}` |
| `securityContext` | Container-level security context | `{}` |

### Service

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Kubernetes service type | `ClusterIP` |
| `service.port` | Service port | `443` |
| `service.targetPort` | Target port on the pod | `8443` |

### Resources and Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `livenessProbe` | Liveness probe configuration | HTTPS GET `/health` |
| `readinessProbe` | Readiness probe configuration | HTTPS GET `/ready` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations (default runs on all nodes including control plane) | `[{operator: Exists, effect: NoSchedule}]` |
| `affinity` | Affinity rules | `{}` |

### Example: Custom Values

```yaml
image:
  repository: myregistry/gopher-cni
  tag: "v1.0.0"

webhook:
  injectedImage: "myregistry/gopher-cni:v1.0.0"
  failurePolicy: Ignore

resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 256Mi

certificate:
  issuer:
    create: false
    name: my-cluster-issuer
    kind: ClusterIssuer

# Run on specific nodes only
nodeSelector:
  node-role.kubernetes.io/worker: "true"
```

Install with custom values:

```bash
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni \
  --create-namespace \
  --values values.yaml
```

## Usage

### Enable Automatic Injection for Pods

Pods opt in to gopher-cni by adding the label and a reference to a WireGuard config secret:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"
  annotations:
    gopher.cni/wgconf-secret: "my-wg-secret"   # Secret containing wg0.conf
spec:
  containers:
  - name: app
    image: myapp:latest
```

The admission webhook will:
- Inject a `gopher-cni-validator` init container that validates the WireGuard tunnel and CNI configuration before the main container starts.
- If the WireGuard config contains a `DNS =` entry, set `dnsPolicy: None` and route all pod DNS through the tunnel resolver.

### Split DNS

To route only specific DNS zones through the cluster DNS server (and everything else through the WireGuard tunnel resolver), add the `gopher.cni/split-tunnel-dns-zones` annotation:

```yaml
annotations:
  gopher.cni/wgconf-secret: "my-wg-secret"
  gopher.cni/split-tunnel-dns-zones: "cluster.local, corp.internal"
```

When this annotation is set the webhook will:
- Inject a CoreDNS sidecar that forwards the listed zones to the cluster DNS server (`kube-dns`) and all other queries to the WireGuard tunnel resolver.
- Set `dnsPolicy: None` and point the pod's nameservers at `127.0.0.1` (the sidecar).

> **Note:** Split DNS requires the WireGuard config to include a `DNS =` entry for the catch-all forwarding to work. Without it, only the listed zones resolve; all other DNS will fail.

> **Note:** If using split DNS, you almost certainly also need `gopher.cni/split-tunnel-cidrs` to include your cluster CIDR so that DNS traffic can reach the cluster DNS server through the tunnel.

## Verification

```bash
# Check certificate
kubectl get certificate -n gopher-cni

# Check DaemonSet
kubectl get daemonset -n gopher-cni

# Check pods (should be one per node)
kubectl get pods -n gopher-cni -o wide

# View logs from a specific node
kubectl logs -n gopher-cni -l app.kubernetes.io/name=gopher-cni --tail=50

# Check webhook configurations
kubectl get mutatingwebhookconfiguration
kubectl get validatingwebhookconfiguration
```

## Troubleshooting

### Certificate Not Ready

```bash
# Check certificate status
kubectl describe certificate -n gopher-cni

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
```

### CA Bundle Not Injected

```bash
# Check ca-injector logs
kubectl logs -n cert-manager deployment/cert-manager-cainjector

# Verify webhook configuration has caBundle
kubectl get mutatingwebhookconfiguration <name> -o yaml | grep caBundle
```

### Webhook Not Mutating Pods

```bash
# Verify label and annotation are present
kubectl get pod <pod-name> -o yaml | grep -A5 'labels:\|annotations:'

# Check webhook logs
kubectl logs -n gopher-cni -l app.kubernetes.io/name=gopher-cni

# Test with example pod
kubectl run test-pod --image=nginx --labels="gopher.cni/enabled=true"
kubectl describe pod test-pod
```

## Upgrading

```bash
helm upgrade gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni
```

## Development

Lint the chart:

```bash
helm lint ./chart/gopher-cni
```

Render templates locally:

```bash
helm template gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni
```

Dry run install:

```bash
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni \
  --create-namespace \
  --dry-run --debug
```

## License

MIT License - See LICENSE file for details
