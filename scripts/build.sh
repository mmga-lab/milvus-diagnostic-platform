#!/bin/bash

set -e

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
IMAGE_NAME="${IMAGE_NAME:-milvus-coredump-agent}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# Build information
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"

echo "Building Milvus Coredump Agent..."
echo "Version: $VERSION"
echo "Build Time: $BUILD_TIME"
echo "Git Commit: $GIT_COMMIT"
echo "Image: $IMAGE_NAME:$IMAGE_TAG"

# Change to root directory
cd "$ROOT_DIR"

# Build Docker image
docker build \
    --build-arg VERSION="$VERSION" \
    --build-arg BUILD_TIME="$BUILD_TIME" \
    --build-arg GIT_COMMIT="$GIT_COMMIT" \
    -t "$IMAGE_NAME:$IMAGE_TAG" \
    .

echo "Build completed successfully!"
echo ""
echo "To deploy the agent, run:"
echo "  kubectl apply -f deployments/"
echo ""
echo "To check the status:"
echo "  kubectl get daemonset milvus-coredump-agent"
echo "  kubectl logs -l app=milvus-coredump-agent"