# Changelog

All notable changes to this project will be documented in this file.

## [0.6.0]

### Added
- `gopher.cni/split-tunnel-cidrs` annotation: comma-separated list of CIDRs to route via the pod's original default interface instead of the WireGuard tunnel, enabling split-tunnel configurations in both `pod-origin` and `host-origin` modes

### Changed
- Refactored WireGuard network setup code: removed dead code, eliminated duplicate logic, and separated interface configuration from route management

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
