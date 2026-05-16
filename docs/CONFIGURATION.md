# Pod Configuration

Gopher CNI is enabled on a per-pod basis using a combination of labels and annotations. Labels are used for enablement and filtering, while annotations provide configuration details.

## Pod Labels
|Label|Description|Required|
|---|---|---|
|`gopher.cni/enabled`|Enables Gopher CNI for this pod. Must be set to `"true"`.|Yes|

The `gopher.cni/enabled` label serves as the primary gate for the Gopher CNI feature. This label is used by the mutating webhook to efficiently filter which pods need processing, avoiding unnecessary webhook invocations for pods that don't use Gopher CNI.

## Pod Annotations
|Annotation|Description|Default|
|---|---|---|
|`gopher.cni/wgconf-secret`|The name of a Kubernetes secret containing WireGuard configuration. See below for Secret definition. Required if `gopher.cni/enabled` label is set to `"true"`.|`""`|
|`gopher.cni/cni-mode`|Which CNI operation mode to use (see below).  Allowed values: `pod-origin`, `host-origin`|`pod-origin`|
|`gopher.cni/dns-tunneled`|Whether to tunnel DNS traffic. DNS server address is taken from the WireGuard config. If not specified in the config, no resolvers will be added.|`true`|


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
    gopher.cni/dns-tunneled: "true"
    gopher.cni/nat-pmp: "true"
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

## DNS Tunneling
The CNI plugin can tunnel DNS traffic through the WireGuard tunnel.  This is enabled by setting the `gopher.cni/dns-tunneled` annotation to `true` (default).  The DNS server address is taken from the WireGuard configuration.

If enabled and the WireGuard configuration does not specify a DNS server, no DNS resolvers will be added to the pod.

If disabled, the pod's DNS resolvers will be used as normal (typically through the cluster's default DNS service).