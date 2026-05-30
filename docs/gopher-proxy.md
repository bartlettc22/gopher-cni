# GopherProxy

GopherProxy lets multiple pods share a single external WireGuard VPN connection. Instead of each pod holding its own VPN credentials, a proxy pod owns the external VPN tunnel and peers route through it.

```
App Pod
  └── wg0 (pod-origin) ──► <name>-proxy Service (ClusterIP)
                                      │
                                Proxy Pod
                                  ├── wg-internal  (WG server — accepts peer pods)
                                  ├── wg-vpn       (WG client — external VPN)
                                  └── ip_forward + MASQUERADE
                                      │
                                External VPN
```

## When to Use It

- You have many pods that all need to egress through the same VPN endpoint.
- You want to keep VPN credentials in one place rather than distributing them to every pod.
- You need to rotate or swap the VPN config without restarting all the pods.

## Prerequisites

The gopher-cni manager must be running (it includes the GopherProxy controller). A `GopherProxy` resource is namespace-scoped, so the proxy and its peer pods must be in the same namespace.

## Setup

### 1. Create the external VPN Secret

The proxy needs a standard WireGuard config for the external VPN. Create a Secret with a `wg.conf` key:

```bash
kubectl create secret generic my-vpn-config \
  --from-file=wg.conf=/path/to/vpn.conf
```

The `wg.conf` should be a complete WireGuard client configuration pointing at the external VPN server:

```ini
[Interface]
PrivateKey = <private key>
Address = 10.200.0.2/32
DNS = 10.200.0.1

[Peer]
PublicKey = <server public key>
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.com:51820
```

### 2. Create the GopherProxy resource

```yaml
apiVersion: gopher.cni/v1alpha1
kind: GopherProxy
metadata:
  name: my-proxy
  namespace: default
spec:
  vpnWGSecret: my-vpn-config        # Secret name from step 1
  internalAddress: 10.100.0.1/24   # Proxy's WireGuard IP; peers get IPs from this subnet
  internalListenPort: 51820         # UDP port (default: 51820)
  peerAllowedIPs:                   # CIDRs peer pods route via the proxy (default: 0.0.0.0/0)
    - 0.0.0.0/0
  peerSelector:                     # Pods matching this label are auto-provisioned
    matchLabels:
      gopher.cni/proxy: my-proxy
```

The controller will:
- Generate an internal WireGuard key pair → `my-proxy-internal-wg` Secret
- Create the proxy pod (`my-proxy-proxy`) with both WireGuard interfaces
- Create a ClusterIP Service (`my-proxy-proxy`) on the internal listen port

### 3. Label your pods

Add `gopher.cni/proxy: my-proxy` to any pod that should route through this proxy. The controller will auto-generate a WireGuard client config Secret for each matching pod.

You also still need the standard Gopher CNI opt-in:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"
    gopher.cni/proxy: my-proxy      # Triggers auto-provisioning by the GopherProxy controller
  annotations:
    gopher.cni/wgconf-secret: my-proxy-peer-my-app-wg   # Auto-generated Secret name
spec:
  containers:
  - name: app
    image: myapp:latest
```

> **Important**: Pods must start *after* the proxy pod is `Running` and the client Secrets exist. The CNI plugin reads the Secret at pod-creation time; if it is missing, the pod will fail to start.

## Spec Reference

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `vpnWGSecret` | string | Yes | — | Name of the Secret containing the external VPN `wg.conf` |
| `internalAddress` | string | Yes | — | WireGuard IP/prefix for the proxy's internal interface (e.g. `10.100.0.1/24`); peer IPs are allocated from this subnet |
| `internalListenPort` | int | No | `51820` | UDP port the proxy's internal WireGuard interface listens on |
| `peerSelector` | LabelSelector | No | — | Selects pods in the same namespace to auto-provision with client config Secrets |
| `peerAllowedIPs` | []string | No | `["0.0.0.0/0"]` | CIDRs that peer pods will route via the proxy |
| `image` | string | No | — | Container image for the proxy pod; defaults to the manager's configured proxy image |

## Status Fields

| Field | Description |
|---|---|
| `phase` | `Pending`, `Running`, or `Failed` |
| `podName` | Name of the managed proxy pod |
| `serviceName` | Name of the ClusterIP Service |
| `internalPublicKey` | WireGuard public key of the proxy's internal interface |
| `peersSecretName` | Name of the Secret holding the hot-reloadable peer list |

Check status with:

```bash
kubectl get gopherproxy my-proxy -o yaml
```

## Managed Resources

The controller creates and owns the following resources (all in the same namespace):

| Resource | Name pattern | Purpose |
|---|---|---|
| Secret | `<name>-internal-wg` | Internal WireGuard key pair |
| Secret | `<name>-peers` | Current peer list; read by the CNI plugin when the proxy pod is (re)created |
| Secret | `<name>-peer-<pod-name>-wg` | Per-pod WireGuard client config (referenced by pod annotation) |
| Pod | `<name>-proxy` | Proxy pod running both WireGuard interfaces |
| Service | `<name>-proxy` | ClusterIP Service for the internal WireGuard listen port |

## Peer Changes

When the controller adds or removes a peer (because a matching pod was created or deleted), it updates the `<name>-peers` Secret and compares a SHA256 hash of its content against the annotation on the running proxy pod. If the hash differs, the controller deletes the proxy pod. The CNI plugin re-creates it on the next scheduling cycle with the new peer list baked in.

Existing peer connections are interrupted briefly during a pod restart. Traffic from peer pods that are still running will reconnect automatically via WireGuard's keepalive.

## Networking Notes

- The proxy pod requires **no special capabilities**. All WireGuard interface creation, ip_forward enablement, routing, and iptables MASQUERADE setup is handled by the gopher-cni CNI plugin (which runs as root on the node) at pod-creation time.
- The CNI plugin applies an iptables `MASQUERADE` rule so the external VPN server sees the proxy's VPN IP, not the peer pod IPs.
- WireGuard adds 80 bytes of overhead per tunnel (20 IP + 8 UDP + 32 WG header + 16 auth tag + 4 type). The MTU on all WireGuard interfaces is set to 1420. Traffic passes through two sequential tunnels (pod→proxy, proxy→VPN) but they are not nested — each tunnel leg independently fits within the standard 1500-byte Ethernet MTU.
- Use `peerAllowedIPs` to limit which traffic goes through the proxy (e.g. only external CIDRs) and pair it with `gopher.cni/split-tunnel-cidrs` on the pods to keep cluster traffic local.

## Complete Example

```yaml
# 1. External VPN config
apiVersion: v1
kind: Secret
metadata:
  name: corp-vpn-config
  namespace: default
stringData:
  wg.conf: |
    [Interface]
    PrivateKey = <private key>
    Address = 10.200.0.2/32
    DNS = 10.200.0.1

    [Peer]
    PublicKey = <server public key>
    AllowedIPs = 0.0.0.0/0
    Endpoint = vpn.corp.example.com:51820
---
# 2. GopherProxy
apiVersion: gopher.cni/v1alpha1
kind: GopherProxy
metadata:
  name: corp-vpn
  namespace: default
spec:
  vpnWGSecret: corp-vpn-config
  internalAddress: 10.100.0.1/24
  peerSelector:
    matchLabels:
      gopher.cni/proxy: corp-vpn
  peerAllowedIPs:
    - 0.0.0.0/0
---
# 3. App pod (after the proxy is Running)
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  namespace: default
  labels:
    gopher.cni/enabled: "true"
    gopher.cni/proxy: corp-vpn
  annotations:
    gopher.cni/wgconf-secret: corp-vpn-peer-my-app-wg
    gopher.cni/split-tunnel-cidrs: "10.96.0.0/12,10.244.0.0/16"  # keep cluster traffic local
    gopher.cni/split-tunnel-dns-zones: "cluster.local"
spec:
  containers:
  - name: app
    image: myapp:latest
```
