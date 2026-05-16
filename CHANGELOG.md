# Changelog

All notable changes to this project will be documented in this file.

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
