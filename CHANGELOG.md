# Changelog

All notable changes to this project will be documented in this file.

## [0.11.0]

### Added
- **GopherProxy** — new `GopherProxy` CRD and controller that creates a WireGuard proxy pod, enabling multiple app pods to share a single external VPN connection. The controller auto-generates per-pod WireGuard client config Secrets and hot-reloads the peer list (via `wg set`) every 5 seconds without proxy restarts
- `gopher.cni/proxy` pod label: pods labeled with a `GopherProxy` name are automatically provisioned as VPN peers; the controller allocates each pod an IP from the proxy's internal subnet and creates a `wg.conf` Secret for the CNI plugin to consume
- Combined `gopher-cni-manager` binary (`cmd/manager/`) runs both the mutating/validating webhook and the new GopherProxy controller in a single Deployment
- New `gopher-cni-proxy` binary (`cmd/proxy/`) runs inside the proxy pod, managing the dual WireGuard interfaces (`wg-internal`, `wg-vpn`) and routing between them with iptables MASQUERADE
- `gopherproxy.yaml` CRD manifest in `config/crd/` and as a Helm template
- Helm chart: new `manager.*` and `proxy.*` image sections in `values.yaml`; expanded RBAC for GopherProxy CRUD, lease management, and event creation

## [0.10.0]

### Fixed
- Incoming traffic on `eth0` was dropped when no split tunneling was configured. Replacing the default route with `wg0` caused asymmetric routing: replies to packets arriving on `eth0` were sent back out `wg0`, which the remote host rejected. Fixed by installing source-based policy routing — a private routing table mirrors the original `eth0` default route, and an `ip rule` per `eth0` address ensures replies are sent back out `eth0`.

## [0.9.0]

### Breaking Changes
- `values.yaml` fully restructured — installer settings under `installer.*`, webhook settings under `webhook.*`; `certificate.*` and `service.*` moved under `webhook.*`; three separate images now required: `installer.image`, `webhook.image`, `sidecar.image`

### Added
- Webhook now runs as a `Deployment` (non-root, `runAsUser: 65534`) instead of on the DaemonSet, enabling independent scaling and privilege separation
- New `gopher-cni-sidecar` image consolidates all injected pod containers (`init-validation`, `write-coredns-config`, and future subcommands); sourced from `sidecar.image` in values
- `ClusterRole`: added `services` to the `get`/`list` rules
- CoreDNS image version pinned to `docker.io/coredns/coredns:1.13.1` in `webhook.config.coreDNSImage`
- All three images (`installer`, `webhook`, `sidecar`) now use `scratch` as the base — static binaries with no OS layer

## [0.8.0]

### Added
- `gopher.cni/split-tunnel-dns-zones` annotation: comma-separated list of DNS zones to resolve via the cluster DNS server (`kube-dns`); all other queries are forwarded to the WireGuard tunnel resolver. When set, the webhook injects a CoreDNS sidecar and sets `dnsPolicy: None` with the pod's nameserver pointed at `127.0.0.1`
- `WEBHOOK_COREDNS_IMAGE` env var to configure the CoreDNS sidecar image (default: `docker.io/coredns/coredns:1.13.1`)
- `write-coredns-config` binary subcommand: writes the `COREFILE` environment variable to `/etc/coredns/Corefile` for use by the CoreDNS config init container

### Changed
- Helm chart: `WEBHOOK_TLS_KEY_PATH` env var is now correctly set from `webhook.tls.keyPath` (was previously missing from the DaemonSet template)

### Removed
- `gopher.cni/dns-tunneled` annotation — DNS tunneling is now always-on when the WireGuard config specifies a DNS server
- `gopher.cni/nat-pmp` annotation — was accepted by the validating webhook but never implemented
- `WEBHOOK_TLS_DISABLE` env var and `webhook.tls.enabled` Helm value — TLS is now mandatory

### Fixed
- Validating webhook now correctly falls back to `req.Namespace` when the pod's `.metadata.namespace` is empty on `CREATE` requests; previously the split-tunnel CIDR overlap check was silently skipped for all newly created pods

## [0.7.0]

### Added
- Protected routes: WireGuard interface addresses and DNS servers are now installed as explicit routes via the WireGuard interface, preventing split-tunnel CIDRs from accidentally capturing that traffic
- `gopher.cni/split-tunnel-overlap` annotation: set to `allow` to permit split-tunnel CIDRs that are less specific than a WireGuard address or DNS server; same/more-specific overlaps are always rejected by the validating webhook
- Validating webhook now enforces split-tunnel CIDR overlap rules against the WireGuard config secret at admission time
- WireGuard config parser now assigns all IPv4 addresses from the `[Interface] Address` block to the WireGuard interface (previously only the first was used)

## [0.6.0]

### Added
- `gopher.cni/split-tunnel-cidrs` annotation: comma-separated list of CIDRs to route via the pod's original default interface instead of the WireGuard tunnel, enabling split-tunnel configurations in both `pod-origin` and `host-origin` modes

### Changed
- Refactored WireGuard network setup code: removed dead code, eliminated duplicate logic, and separated interface configuration from route management
- Webhook DNS injection now filters to IPv4 addresses only, as IPv6 tunnel DNS is not currently supported

## [0.5.0]

### Fixed
- Integration test conflist-clean check now greps for `"type": "gopher-cni"` (the literal JSON plugin-type field) instead of the bare string `gopher-cni`, preventing false positives from the cluster name appearing in the Calico conflist
- Integration test conflist-clean check now polls with a 30-second timeout instead of asserting once, handling the brief bind-mount visibility lag in k3d after pod exit

### Added
- Integration test assertion that the underlying CNI conflist (e.g. Calico's `10-calico.conflist`) still exists after uninstall — gopher-cni should strip its plugin entry, not delete the file
- Taskfile task descriptions

## [0.4.0]

### Fixed
- `Uninstall()` now passes the CNI net directory (not the config file path) to `uninstallCNIConfig`, so the gopher-cni entry is correctly removed from the conflist on pod shutdown — previously the entry was silently left in place
- `Uninstall()` now prepends `MountedHostDir` when locating the CNI binary for removal, so the binary is actually deleted from the host filesystem instead of a non-existent container path
- Integration test pods now set `terminationGracePeriodSeconds: 0` so namespace cleanup during tests does not wait 30 seconds for `sleep 3600` pods to terminate

### Added
- Unit test for `Uninstall()` in `pkg/install-cni` covering correct host-path removal of the kubeconfig, binary, and gopher-cni conflist entry

## [0.3.0]

### Changed
- WireGuard config parser now supports multiple comma-separated `Address` entries; selects the first IPv4 address (IPv6 and multiple address support pending)

## [0.2.0]

### Added
- Helm chart README: document PodSecurity `privileged` namespace label requirement for clusters enforcing baseline/restricted policy
- Helm chart README: add OCI registry install instructions (`oci://ghcr.io/bartlettc22/charts/gopher-cni`)

### Changed
- k3d installed via direct GitHub release binary download instead of the install script
- CI `publish` job: use `docker/setup-buildx-action@v4` to enable multi-platform builds
- CI `build` job: removed unnecessary `setup-go` step (Go runs inside Docker, not on the runner)

### Removed
- Validating webhook: removed controller-level webhook (`validate-controllers.gopher-cni.io`) targeting Deployments, StatefulSets, and DaemonSets — pod-level validation is sufficient since pods always pass through the pod webhook on creation
- Dead code: removed Deployment, StatefulSet, and DaemonSet handling from `pkg/webhook/validate.go`

## [0.1.0]

### Added
- Initial release
