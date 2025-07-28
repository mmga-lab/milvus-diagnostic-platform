# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go modules files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
    -o milvus-coredump-agent \
    ./cmd/agent

# Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    gdb \
    binutils \
    file \
    procps \
    util-linux \
    helm \
    && rm -rf /var/cache/apk/*

# Create non-root user (though we'll run as root for system access)
RUN addgroup -g 1000 agent && \
    adduser -D -s /bin/sh -u 1000 -G agent agent

# Create directories
RUN mkdir -p /data/coredumps /etc/agent && \
    chown -R agent:agent /data

# Copy binary from builder
COPY --from=builder /app/milvus-coredump-agent /bin/milvus-coredump-agent

# Copy default configuration
COPY --from=builder /app/configs/config.yaml /etc/agent/config.yaml

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/healthz || exit 1

# Set labels
LABEL org.opencontainers.image.title="Milvus Coredump Agent"
LABEL org.opencontainers.image.description="Kubernetes DaemonSet agent for collecting and analyzing Milvus coredump files"
LABEL org.opencontainers.image.vendor="Milvus"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_TIME}"

# Run as root to access system resources
USER root

ENTRYPOINT ["/bin/milvus-coredump-agent"]
CMD ["--config=/etc/agent/config.yaml"]