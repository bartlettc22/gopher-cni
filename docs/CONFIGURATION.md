# Pod Configuration

Gopher CNI is enabled on a per-pod basis using a combination of labels and annotations. Labels are used for enablement and filtering, while annotations provide configuration details.

## Pod Labels
|Label|Description|Required|
|---|---|---|
|`gopher.cni/enabled`|Enables Gopher CNI for this pod. Must be set to `"true"`.|Yes|
|`gopher.cni/proxy`|Name of a `GopherProxy` resource in the same namespace. The GopherProxy controller auto-generates a WireGuard client config Secret for this pod and registers it as a VPN peer. Set `gopher.cni/wgconf-secret` to `<proxy-name>-peer-<pod-name>-wg` to reference it.|No|

The `gopher.cni/enabled` label serves as the primary gate for the Gopher CNI feature. This label is used by the mutating webhook to efficiently filter which pods need processing, avoiding unnecessary webhook invocations for pods that don't use Gopher CNI.

When using a `GopherProxy` (see [GopherProxy](gopher-proxy.md)), the WireGuard Secret is auto-generated — you do not create it manually. Set `gopher.cni/wgconf-secret` to `<proxy-name>-peer-<pod-name>-wg` and the controller will create it before the pod starts.

## Pod Annotations
|Annotation|Description|Default|
|---|---|---|
|`gopher.cni/wgconf-secret`|The name of a Kubernetes secret containing WireGuard configuration. See below for Secret definition. Required if `gopher.cni/enabled` label is set to `"true"`.|`""`|
|`gopher.cni/cni-mode`|Which CNI operation mode to use (see below).  Allowed values: `pod-origin`, `host-origin`|`pod-origin`|
|`gopher.cni/split-tunnel-cidrs`|Comma-separated list of CIDRs to route via the pod's original default interface instead of the WireGuard tunnel. See [Split Tunneling](#split-tunneling) below.|`""`|
|`gopher.cni/split-tunnel-overlap`|Controls how the webhook handles split-tunnel CIDRs that overlap with WireGuard addresses or DNS servers. Set to `allow` to permit less-specific overlaps. See [Split Tunneling](#split-tunneling) below.|`""`|
|`gopher.cni/split-tunnel-dns-zones`|Comma-separated list of DNS zones to resolve via the cluster DNS server instead of the WireGuard tunnel DNS. See [Split DNS](#split-dns) below.|`""`|


### Complete Pod Example
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  labels:
    gopher.cni/enabled: "true"
  annotations:
    gopher.cni/wgconf-secret: "my-wireguard-config"
    gopher.cni/cni-mode: "host-origin"
    gopher.cni/split-tunnel-cidrs: "10.96.0.0/12,10.244.0.0/16"
    gopher.cni/split-tunnel-overlap: "allow"
    gopher.cni/split-tunnel-dns-zones: "cluster.local,svc.cluster.local"
spec:
  containers:
  - name: app
    image: my-app:latest
```

## WireGuard Secret
Gopher CNI obtains the WireGuard tunnel configuration through the `gopher.cni/wgconf-secret` annotation. The annotation value should be the name of an existing Kubernetes secret inside the same namespace as the pod. The secret should have a key named `wg.conf` which contains the WireGuard configuration in INI format.

Example contents of the secret:
```ini
[Interface]
PrivateKey = <private key>
Address = <address>
DNS = <DNS server>

[Peer]
PublicKey = <public key>
AllowedIPs = <allowed IPs>
Endpoint = <endpoint>
```

**Important**: Both the label and the `wgconf-secret` annotation are required. The label enables efficient webhook filtering and clearly identifies pods using Gopher CNI, while the annotation specifies the WireGuard configuration.

## CNI Operation Modes
The CNI plugin can operate in two modes. The mode is specified by the `gopher.cni/cni-mode` annotation and can contain one of the following values:
* `pod-origin`
* `host-origin`

### Pod-Origin Mode (default)
In `pod-origin` mode, the CNI plugin creates a WireGuard interface whose "birthplace" is inside the pod's network namespace. 

In this mode, all pod traffic is routed into the WireGuard tunnel and leaves the pod as WireGuard-encapsulated UDP through the original `eth0` interface. Because the encrypted packets egress via `eth0`, Kubernetes NetworkPolicies and other node-level networking controls apply to them normally. If applying egress NetworkPolicies to the pod, ensure the WireGuard peer endpoint IP and UDP port are allowed.

Benefits:
* With proper configuration, traffic can still reach cluster resources or other network endpoints outside of the tunnel
* Network policies can be used to control traffic and ensure that all traffic exiting the pod is through the tunnel
* Encrypted tunnel traffic passes through the standard Kubernetes overlay network stack

Considerations:
* Pod's original network interface must be left intact for connectivity
* Non-tunnelled traffic can exit the pod through the pod's default interface (ex: `eth0`) if precautions are not taken such as creating Kubernetes network policies enforcing egress to only the WireGuard peer endpoint IP.

See https://www.wireguard.com/netns/ for more details.

### Host-Origin Mode
In `host-origin` mode, the CNI plugin creates a WireGuard interface whose "birthplace" is outside the container and moved into the pod's network namespace.

In this mode, normal pod traffic will be tunneled and routed using the host's default route.  This bypasses the Kubernetes overlay network completely.

Benefits:
* Reduced network latency by avoiding the Kubernetes overlay network
* Original pod interface can be removed (future feature) to guarantee all pod traffic is tunneled without having to rely on Kubernetes network policies

Considerations:
* Non-tunnelled traffic can exit the pod through the pod's default interface (ex: `eth0`), if not removed (future feature)

See https://www.wireguard.com/netns/ for more details.

## Split Tunneling
Split tunneling allows specific CIDRs to bypass the WireGuard tunnel and be routed through the pod's original default interface instead. All other traffic continues to flow through WireGuard.

This is configured via the `gopher.cni/split-tunnel-cidrs` annotation, which accepts a comma-separated list of CIDRs:

```yaml
annotations:
  gopher.cni/split-tunnel-cidrs: "10.96.0.0/12,10.244.0.0/16"
```

A common use case is to keep traffic to cluster-internal networks (e.g. the Kubernetes service CIDR and pod CIDR) routed locally while sending all other traffic through the tunnel. This works in both `pod-origin` and `host-origin` modes.

### Protected Routes
To guarantee WireGuard addresses and DNS servers are always reachable through the tunnel, the CNI plugin automatically installs explicit routes via the WireGuard interface (`gcni0`) for:
- Each IPv4 address from the WireGuard `[Interface] Address` block, at its configured prefix length
- Each IPv4 DNS server from the WireGuard config, as a `/32` host route

These explicit routes take precedence over any less-specific split-tunnel routes via longest-prefix-match.

### Overlap Validation
The webhook validates split-tunnel CIDRs against the WireGuard addresses and DNS servers from the pod's WireGuard secret. There are two tiers of enforcement:

**Always rejected** — if a split-tunnel CIDR is the same prefix length or more specific than a protected route (a WireGuard address CIDR or a DNS server `/32`), the pod will be rejected unconditionally. In this case, the split-tunnel route would win over the explicit protected route and WireGuard connectivity or DNS tunneling would break.

**Rejected by default, overridable** — if a split-tunnel CIDR merely overlaps with a protected route but is less specific, the pod is rejected by default. The explicit protected routes installed by the CNI plugin will ensure correct routing, but the overlap must be acknowledged explicitly by adding:

```yaml
annotations:
  gopher.cni/split-tunnel-cidrs: "10.0.0.0/8"
  gopher.cni/split-tunnel-overlap: "allow"
```

For example, with a WireGuard address of `10.2.0.0/24` and DNS server `10.2.0.1`:
| Split-tunnel CIDR | Overlap type | Result |
|---|---|---|
| `192.168.0.0/16` | None | Allowed |
| `10.0.0.0/8` | Less specific than `/24` and `/32` | Rejected by default; allowed with `split-tunnel-overlap=allow` |
| `10.2.0.0/24` | Same specificity as WireGuard address | Always rejected |
| `10.2.0.1/32` | Same specificity as DNS `/32` | Always rejected |
| `10.2.0.5/32` | More specific than WireGuard `/24` | Always rejected |

## DNS Tunneling
When the WireGuard configuration includes a `DNS =` entry, the CNI plugin automatically configures the pod to use that DNS server. All DNS queries from the pod are routed through the WireGuard tunnel to the tunnel's DNS resolver.

If the WireGuard configuration does not specify a DNS server, the pod's DNS configuration is left unchanged. In most cases this will cause all DNS resolution to fail, because the cluster's internal DNS server (e.g. `kube-dns`) is not reachable through the WireGuard tunnel. It is strongly recommended to always include a `DNS =` entry in the WireGuard configuration, or to use `gopher.cni/split-tunnel-dns-zones` to keep cluster DNS reachable.

## Split DNS
Split DNS allows specific DNS zones to be resolved by the cluster DNS server (typically `kube-dns`) while all other DNS traffic continues through the WireGuard tunnel resolver. This is useful for pods that need to resolve both cluster-internal names (e.g. Kubernetes services) and external names via the VPN.

This is configured via the `gopher.cni/split-tunnel-dns-zones` annotation:

```yaml
annotations:
  gopher.cni/wgconf-secret: "my-wg-config"
  gopher.cni/split-tunnel-dns-zones: "cluster.local,svc.cluster.local"
```

When this annotation is set, the mutating webhook:
1. Injects a CoreDNS sidecar container into the pod.
2. Generates a CoreDNS configuration that forwards the listed zones to the cluster DNS server (discovered from the `kube-dns` service in `kube-system`) and forwards all other queries to the WireGuard tunnel's DNS resolver.
3. Sets the pod's `dnsPolicy` to `None` and `dnsConfig.nameservers` to `127.0.0.1` so all DNS queries go through the sidecar.

The annotation value is a comma-separated list of zone suffixes. A trailing dot is optional; both `cluster.local` and `cluster.local.` are accepted.

> **Note**: If the WireGuard configuration does not include a `DNS =` entry, queries for the listed zones will still be forwarded to the cluster DNS server, but all other DNS resolution will fail.

> **Note**: Because DNS queries to the cluster DNS server must reach the cluster network (e.g. the `kube-dns` service IP), you almost certainly need to pair `split-tunnel-dns-zones` with `split-tunnel-cidrs` that includes your cluster's service and pod CIDRs. Without this, DNS traffic destined for `kube-dns` will be routed into the WireGuard tunnel and will not reach the cluster DNS server.