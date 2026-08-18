# Multi-stage build for telegram-notifier
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo \
    -o telegram-notifier ./cmd/telegram-notifier

# Final stage: Alpine runtime with shell for liveness probe
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Create non-root user
RUN addgroup -g 1000 appgroup && adduser -D -u 1000 -G appgroup -s /sbin/nologin appuser

# Copy the binary from builder
COPY --from=builder /build/telegram-notifier /app/telegram-notifier

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

ENTRYPOINT ["/app/telegram-notifier"]
