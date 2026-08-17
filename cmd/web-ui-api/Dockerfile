# Multi-stage build for web-ui-api

# Stage 1: Build
FROM golang:1.25.0-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .

# Build the web-ui-api binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o web-ui-api ./cmd/web-ui-api

# Stage 2: Runtime
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder
COPY --from=builder /build/web-ui-api .

# Expose the default port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/healthz || exit 1

# Run the service
CMD ["./web-ui-api"]
