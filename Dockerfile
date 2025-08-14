# -----------------------------------------------------------------------------
# Production-hardened multi-stage Dockerfile for Xray (VLESS/XTLS)
# - Verifies XRAY binary via optional SHA256 build-arg
# - Builds a tiny static Go entrypoint to supervise Xray & expose /health
# - Produces a minimal runtime image using distroless static:nonroot
# -----------------------------------------------------------------------------
# Usage example:
# docker build --build-arg XRAY_VERSION=v25.8.3 --build-arg XRAY_SHA256=<sha256sum> -t ghcr.io/you/xray-node:v25.8.3 .
#
ARG XRAY_VERSION="v25.8.3"
ARG XRAY_SHA256=""
FROM alpine:3.19 AS fetch
ARG XRAY_VERSION
ARG XRAY_SHA256

# minimal tools to download & verify
RUN apk add --no-cache curl unzip ca-certificates

WORKDIR /tmp
# Note: adjust the URL if Xray release asset naming changes.
ARG XRAY_DL="https://github.com/XTLS/Xray-core/releases/download/${XRAY_VERSION}/Xray-linux-64.zip"

# Download and verify (if XRAY_SHA256 supplied)
RUN set -eux; \
    curl -fsSL -o xray.zip "${XRAY_DL}"; \
    unzip -q xray.zip -d xray-tmp; \
    mv xray-tmp/xray /tmp/xray-x; \
    if [ -n "${XRAY_SHA256}" ]; then \
    echo "${XRAY_SHA256}  /tmp/xray-x" > /tmp/xray.sha256; \
    sha256sum -c /tmp/xray.sha256; \
    fi; \
    chmod 0755 /tmp/xray-x

# -----------------------------------------------------------------------------
# build tiny static entrypoint binary (supervisor + health)
# -----------------------------------------------------------------------------
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache git build-base
WORKDIR /src
COPY entrypoint.go .
# Build statically for maximum portability; strip symbols
# Disable cgo to avoid glibc dependency
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -o /out/entrypoint entrypoint.go

# -----------------------------------------------------------------------------
# Final image: distroless static nonroot for minimal attack surface
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static:nonroot AS runtime

# Pass ARG to runtime stage
ARG XRAY_VERSION

# create working dir structure (files copied into image already owned by nonroot)
COPY --from=fetch /tmp/xray-x /usr/local/bin/xray
COPY --from=builder /out/entrypoint /usr/local/bin/entrypoint

# default config shipped as example; in production mount /etc/xray/config.json:ro
COPY config.json /etc/xray/config.json

# runtime file locations; /var/log needs to be writable by the runtime user
# distroless nonroot contains uid 65532; we rely on host mounts for logs or create tmp volumes at runtime.
# Expose health port (unprivileged) and application port for passthrough (container ports are informational)
EXPOSE 8080 443

# Metadata & labels (helpful for scanning & provenance)
LABEL org.opencontainers.image.title="xray-node" \
    org.opencontainers.image.description="Hardened Xray VLESS node (non-root, minimal image)" \
    org.opencontainers.image.version="${XRAY_VERSION}" \
    org.opencontainers.image.licenses="MIT"

# Runtime entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint"]
