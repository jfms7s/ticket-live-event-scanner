# Multi-stage build for telegram-notifier
FROM golang:1.22-alpine AS builder

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

# Final stage: minimal scratch image
FROM scratch

# Copy the binary from builder
COPY --from=builder /build/telegram-notifier /telegram-notifier

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/telegram-notifier"]
