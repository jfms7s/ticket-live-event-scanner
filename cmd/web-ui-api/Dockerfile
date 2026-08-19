# Multi-stage build for web-ui-api

# Stage 1: Build
FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .

# TARGETOS/TARGETARCH are populated automatically by buildx per platform in
# the build matrix (see .github/workflows/build-push.yml's `platforms:`) --
# building on $BUILDPLATFORM and cross-compiling to $TARGETARCH is what
# makes multi-platform builds fast (no QEMU-emulated compile step).
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o web-ui-api ./cmd/web-ui-api

# Stage 2: Runtime
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder
COPY --from=builder /build/web-ui-api .

# /data is used for a local libSQL file DB in docker-compose (Turso is used
# in k8s instead); create it here so it's owned by the non-root user once a
# volume is mounted over it.
RUN mkdir -p /data && chown 1000:1000 /data

# Run as non-root user (defense-in-depth; k8s securityContext also enforces this)
USER 1000:1000

# Expose the default port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/healthz || exit 1

# Run the service
CMD ["./web-ui-api"]
