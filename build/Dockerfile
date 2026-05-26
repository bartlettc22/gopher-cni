FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

# Install build dependencies
RUN apk add --no-cache git make

# Copy source code
COPY . /src
WORKDIR /src

# Build the binaries using Go's native cross-compilation.
# The build stage runs on the host platform (BUILDPLATFORM) so no QEMU is
# needed; GOOS/GOARCH target the final image's platform (TARGETPLATFORM).
RUN --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /gopher-installer ./cmd/installer
RUN --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /gopher-webhook ./cmd/webhook
RUN --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /gopher-sidecar ./cmd/sidecar
RUN --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /plugin ./plugin

# Installer image — runs as root on each node to install the CNI plugin binary
# and config onto the host filesystem via hostPath volume mounts.
FROM alpine:3.23.4 AS installer

RUN apk add --no-cache ca-certificates

ENV CNI_BIN_SOURCE_DIR=/cni/gopher-cni

COPY --from=build /gopher-installer /gopher-installer
COPY --from=build /plugin ${CNI_BIN_SOURCE_DIR}/gopher-cni

ENTRYPOINT ["/gopher-installer"]

# Webhook image — runs as non-root; serves the mutating/validating admission
# webhook and does not require access to the host filesystem.
FROM alpine:3.23.4 AS webhook

RUN apk add --no-cache ca-certificates

COPY --from=build /gopher-webhook /gopher-webhook

USER 65534

ENTRYPOINT ["/gopher-webhook"]

# Sidecar image — injected into user pods; runs init-validation, write-coredns-config,
# and future subcommands (e.g. nat-pmp). No host filesystem access required.
FROM alpine:3.23.4 AS sidecar

RUN apk add --no-cache ca-certificates

COPY --from=build /gopher-sidecar /gopher-sidecar

USER 65534

ENTRYPOINT ["/gopher-sidecar"]
