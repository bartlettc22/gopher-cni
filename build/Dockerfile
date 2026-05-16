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
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /gopher ./cmd/main
RUN --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags "-s -w -extldflags \"-static\" -X 'github.com/bartlettc22/gopher-cni/pkg/version.Version=${VERSION}'" -o /plugin ./plugin

# Final stage - minimal runtime image
FROM alpine:3.23.4

# Install CA certificates for HTTPS
RUN apk add --no-cache ca-certificates

ENV CNI_BIN_SOURCE_DIR=/cni/gopher-cni

# Copy binaries from builder
COPY --from=build /gopher /gopher
COPY --from=build /plugin ${CNI_BIN_SOURCE_DIR}/gopher-cni

# Running as root is required to write the CNI plugin binary and config to the
# host filesystem via hostPath volume mounts.
# TODO: split into separate init (root, installs CNI files) and daemon
# (non-root, serves webhook) images so the long-running process drops privileges.

ENTRYPOINT ["/gopher"]
CMD ["daemon"]
