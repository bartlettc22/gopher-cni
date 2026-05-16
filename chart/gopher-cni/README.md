# Gopher CNI Helm Chart

A Helm chart for deploying Gopher CNI to Kubernetes. Gopher CNI is a CNI plugin that tunnels pod traffic through WireGuard VPN with automatic network validation and optional NAT-PMP port forwarding support. This chart deploys the admission webhook component for automatic injection of validation and sidecar containers.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- [cert-manager](https://cert-manager.io/) installed in the cluster

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
# Add the repository (if published)
helm repo add gopher-cni https://bartlettc22.github.io/gopher-cni

# Install the chart
helm install gopher-cni gopher-cni/gopher-cni \
  --namespace gopher-cni-system \
  --create-namespace

# Or install from local chart
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni-system \
  --create-namespace
```

## Uninstalling the Chart

```bash
helm uninstall gopher-cni --namespace gopher-cni-system
```

## Configuration

The following table lists the configurable parameters of the Gopher CNI chart and their default values.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `gopher-cni` |
| `image.tag` | Image tag | `""` (uses chart appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `webhook.port` | Webhook server port | `8443` |
| `webhook.injectedImage` | Image for injected containers | `gopher-cni:latest` |
| `webhook.cni.binPath` | Host path for CNI binaries | `/opt/cni/bin` |
| `webhook.cni.configPath` | Host path for CNI config | `/etc/cni/net.d` |
| `webhook.failurePolicy` | Webhook failure policy | `Fail` |
| `webhook.timeoutSeconds` | Webhook timeout | `10` |
| `certificate.enabled` | Enable cert-manager certificates | `true` |
| `certificate.duration` | Certificate duration | `2160h` (90 days) |
| `certificate.renewBefore` | Renew before expiry | `360h` (15 days) |
| `certificate.issuer.create` | Create self-signed issuer and CA | `true` |
| `certificate.issuer.name` | Issuer name (if not creating) | `""` |
| `certificate.issuer.kind` | Issuer kind | `Issuer` |
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |

### Example: Custom Values

Create a `values.yaml` file:

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
  --namespace gopher-cni-system \
  --create-namespace \
  --values values.yaml
```

## Usage

### Enable Automatic Validation for Pods

Pods using the Gopher CNI plugin can opt-in to automatic validation by adding the label:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"             # Enable automatic validation
spec:
  containers:
  - name: app
    image: myapp:latest
```

The admission webhook will:
- Inject an init container to validate the WireGuard tunnel and CNI configuration before the main container starts

Note: This chart deploys the daemon mode which installs the CNI plugin and runs the webhook server.

### Disable Webhook for Namespace

```bash
kubectl label namespace my-namespace gopher-cni.io/webhook=disabled
```

## Verification

Check DaemonSet and pod status:

```bash
# Check certificate
kubectl get certificate -n gopher-cni-system

# Check DaemonSet
kubectl get daemonset -n gopher-cni-system

# Check pods (should be one per node)
kubectl get pods -n gopher-cni-system -o wide

# View logs from a specific node
kubectl logs -n gopher-cni-system -l app.kubernetes.io/name=gopher-cni --tail=50

# Check webhook configurations
kubectl get mutatingwebhookconfiguration
kubectl get validatingwebhookconfiguration
```

## Troubleshooting

### Certificate Not Ready

```bash
# Check certificate status
kubectl describe certificate -n gopher-cni-system

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
# Verify label is present
kubectl get pod <pod-name> -o yaml | grep gopher.cni/enabled

# Check webhook logs
kubectl logs -n gopher-cni-system -l app.kubernetes.io/name=gopher-cni

# Test with example pod
kubectl run test-pod --image=nginx --labels="gopher.cni/enabled=true"
kubectl describe pod test-pod
```

## Upgrading

```bash
helm upgrade gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni-system
```

## Development

Lint the chart:

```bash
helm lint ./chart/gopher-cni
```

Render templates locally:

```bash
helm template gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni-system
```

Dry run install:

```bash
helm install gopher-cni ./chart/gopher-cni \
  --namespace gopher-cni-system \
  --create-namespace \
  --dry-run --debug
```

## License

MIT License - See LICENSE file for details
