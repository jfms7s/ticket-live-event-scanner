# Multi-stage build for web-ui-api

# Stage 1: Build the dashboard. The output is architecture-independent, so
# build natively instead of under QEMU for arm64.
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend

WORKDIR /app

COPY web/frontend/package.json web/frontend/package-lock.json ./
RUN npm ci

COPY web/frontend/tsconfig*.json web/frontend/vite.config.ts web/frontend/index.html ./
COPY web/frontend/src/ src/
RUN npm run build

# Stage 2: Build the Go binary with the dashboard embedded (web/frontend/embed.go)
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .
COPY --from=frontend /app/dist web/frontend/dist

# TARGETOS/TARGETARCH are populated automatically by buildx per platform in
# the build matrix (see .github/workflows/build-push.yml's `platforms:`) --
# building on $BUILDPLATFORM and cross-compiling to $TARGETARCH is what
# makes multi-platform builds fast (no QEMU-emulated compile step).
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s" -o web-ui-api ./cmd/web-ui-api

# Stage 3: Runtime
FROM alpine:3.24

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder
COPY --from=builder /build/web-ui-api .

# Run as non-root user (defense-in-depth; k8s securityContext also enforces this)
USER 1000:1000

# Expose the default port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/healthz || exit 1

# Run the service
CMD ["./web-ui-api"]
