# Antigravity Claude Proxy - Go Backend
# Multi-stage Dockerfile for minimal production image

# ============================================================
# Stage 1: Build
# ============================================================
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/antigravity-proxy \
    ./cmd/server

# Build the accounts CLI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/antigravity-accounts \
    ./cmd/accounts

# ============================================================
# Stage 2: Runtime
# ============================================================
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/antigravity-proxy /app/
COPY --from=builder /app/antigravity-accounts /app/

# Copy frontend assets from parent directory
# This should be mounted or copied during build context setup
COPY --from=builder /app/../public /app/public

# Create config directory
RUN mkdir -p /home/appuser/.antigravity-claude-proxy && \
    chown -R appuser:appuser /home/appuser

# Switch to non-root user
USER appuser

# Expose ports
EXPOSE 8080

# Environment variables
ENV PORT=8080 \
    HOST=0.0.0.0 \
    REDIS_ADDR=redis:6379

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health || exit 1

# Run the server
ENTRYPOINT ["/app/antigravity-proxy"]
