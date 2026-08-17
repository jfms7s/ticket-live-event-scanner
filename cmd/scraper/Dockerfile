# Builder stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /build/scraper ./cmd/scraper

# Final stage
FROM alpine:3.20

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/scraper /app/scraper

# Non-root user
RUN addgroup -D -g 1000 scraper && adduser -D -u 1000 scraper -G scraper
USER scraper

ENTRYPOINT ["/app/scraper"]
