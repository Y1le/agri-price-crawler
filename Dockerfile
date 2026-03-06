# Build stage
FROM golang:1.24-alpine AS builder

# Install git for Go modules and ca-certificates for secure connections
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Set build environment variables
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

# Build the binary for Linux
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o agri-price-crawler cmd/craw-server/crawserver.go

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Create non-root user for security
RUN addgroup -g 65532 nonroot &&\
    adduser -D -u 65532 -G nonroot nonroot

# Copy the binary from builder stage
COPY --from=builder /app/agri-price-crawler .

# Copy certificates if they exist (optional)
COPY --from=builder /app/craw.pem . 2>/dev/null || true
COPY --from=builder /app/craw-key.pem . 2>/dev/null || true

# Create directory for logs
RUN mkdir -p /app/var && chown -R nonroot:nonroot /app/var

# Switch to non-root user
USER nonroot

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./agri-price-crawler"]