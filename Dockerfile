# syntax=docker/dockerfile:1

# Build stage: cross-compiles for the target platform, so multi-arch images
# build natively on the build host instead of under emulation.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Injected the same way GoReleaser stamps release binaries; empty values fall
# back to the "dev" version.
ARG VERSION=
ARG COMMIT=
ARG DATE=
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X github.com/dgrieser/jw-cli/internal/version.Version=${VERSION} \
        -X github.com/dgrieser/jw-cli/internal/version.Commit=${COMMIT} \
        -X github.com/dgrieser/jw-cli/internal/version.Date=${DATE}" \
      -o /out/jw ./cmd/jw \
 && mkdir -p /out/data/cache

# Runtime stage: distroless carries CA certificates and a nonroot user, and
# nothing else — the binary is static.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/jw /jw
# /data holds the on-disk cache (language lists, wol config, documents).
# Mount a volume to keep it across container restarts; without one the cache
# simply lives and dies with the container.
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV XDG_CACHE_HOME=/data/cache

VOLUME /data
EXPOSE 8080

# 0.0.0.0 on purpose: inside the container network, port mapping (-p) is the
# boundary. The server has no authentication — publish the port thoughtfully.
ENTRYPOINT ["/jw"]
CMD ["serve", "--addr", "0.0.0.0", "--port", "8080"]
