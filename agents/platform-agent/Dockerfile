# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files first for better caching.
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code.
COPY . .

# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /platform-agent \
    ./cmd/agent

# Final stage
FROM scratch

# Copy CA certificates for HTTPS.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary.
COPY --from=builder /platform-agent /platform-agent

# Create state directory (will be overwritten by volume mount).
VOLUME /var/lib/platform-agent

# Run as non-root user.
USER 65534:65534

ENTRYPOINT ["/platform-agent"]
