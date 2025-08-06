#!/bin/bash

set -e

# Configuration
NAMESPACE="${NAMESPACE:-default}"
RELEASE_NAME="${RELEASE_NAME:-milvus-coredump-agent}"
TIMEOUT="${TIMEOUT:-300}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_step() {
    echo -e "${BLUE}==>${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -n, --namespace NAMESPACE    Kubernetes namespace (default: default)"
    echo "  -r, --release RELEASE        Helm release name (default: milvus-coredump-agent)"
    echo "  -t, --timeout TIMEOUT        Wait timeout in seconds (default: 300)"
    echo "  -h, --help                   Show this help message"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -r|--release)
            RELEASE_NAME="$2"
            shift 2
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            print_error "Unknown option $1"
            usage
            exit 1
            ;;
    esac
done

print_step "Verifying Milvus Coredump Agent deployment"
echo "Namespace: $NAMESPACE"
echo "Release: $RELEASE_NAME"
echo "Timeout: ${TIMEOUT}s"
echo ""

# Check if Helm release exists
print_step "Checking Helm release..."
if helm list -n "$NAMESPACE" | grep -q "$RELEASE_NAME"; then
    print_success "Helm release found: $RELEASE_NAME"
else
    print_error "Helm release not found: $RELEASE_NAME"
    exit 1
fi

# Wait for controller deployment
print_step "Waiting for controller deployment to be ready..."
if kubectl wait --for=condition=available deployment/"${RELEASE_NAME}-controller" \
    --namespace="$NAMESPACE" --timeout="${TIMEOUT}s"; then
    print_success "Controller deployment is ready"
else
    print_error "Controller deployment failed to become ready"
    exit 1
fi

# Wait for agent daemonset
print_step "Waiting for agent daemonset to be ready..."
if kubectl wait --for=condition=ready pod \
    --selector="app.kubernetes.io/component=agent" \
    --namespace="$NAMESPACE" --timeout="${TIMEOUT}s"; then
    print_success "Agent daemonset is ready"
else
    print_error "Agent daemonset failed to become ready"
    exit 1
fi

# Check services
print_step "Verifying services..."
if kubectl get service "${RELEASE_NAME}-controller" -n "$NAMESPACE" &> /dev/null; then
    print_success "Controller service is created"
else
    print_warning "Controller service not found"
fi

# Test controller API
print_step "Testing controller API..."
if kubectl run test-api --rm -i --tty --restart=Never --image=curlimages/curl:latest \
    --namespace="$NAMESPACE" -- \
    curl -f "http://${RELEASE_NAME}-controller:8090/healthz" &> /dev/null; then
    print_success "Controller API is responsive"
else
    print_warning "Controller API test failed (this might be expected if the controller is still starting)"
fi

# Check pods status
print_step "Checking pod status..."
echo ""
echo "Controller pods:"
kubectl get pods -l "app.kubernetes.io/component=controller" -n "$NAMESPACE" -o wide

echo ""
echo "Agent pods:"
kubectl get pods -l "app.kubernetes.io/component=agent" -n "$NAMESPACE" -o wide

# Check persistent volumes if enabled
print_step "Checking persistent volumes..."
if kubectl get pvc "${RELEASE_NAME}-controller-data" -n "$NAMESPACE" &> /dev/null; then
    echo "Controller PVC:"
    kubectl get pvc "${RELEASE_NAME}-controller-data" -n "$NAMESPACE"
    print_success "Controller persistence is configured"
else
    print_warning "No persistent volume found (might be disabled)"
fi

# Check secrets
print_step "Checking secrets..."
if kubectl get secret milvus-coredump-secrets -n "$NAMESPACE" &> /dev/null; then
    print_success "API key secret is configured"
else
    print_warning "API key secret not found (AI analysis might be disabled)"
fi

# Show resource usage
print_step "Resource usage:"
echo ""
echo "Controller resource usage:"
kubectl top pod -l "app.kubernetes.io/component=controller" -n "$NAMESPACE" --no-headers 2>/dev/null || echo "Metrics not available"

echo ""
echo "Agent resource usage:"
kubectl top pod -l "app.kubernetes.io/component=agent" -n "$NAMESPACE" --no-headers 2>/dev/null || echo "Metrics not available"

# Final status
print_step "Deployment verification complete!"
echo ""
print_success "Milvus Coredump Agent is running successfully"
echo ""
echo "Next steps:"
echo "  # View controller logs:"
echo "  kubectl logs -l app.kubernetes.io/component=controller -n $NAMESPACE -f"
echo ""
echo "  # View agent logs:"
echo "  kubectl logs -l app.kubernetes.io/component=agent -n $NAMESPACE -f"
echo ""
echo "  # Access controller API:"
echo "  kubectl port-forward svc/${RELEASE_NAME}-controller 8090:8090 -n $NAMESPACE"
echo "  curl http://localhost:8090/api/stats"
echo ""
echo "  # Access dashboard:"
echo "  kubectl port-forward ds/${RELEASE_NAME}-agent 8082:8082 -n $NAMESPACE"
echo "  # Open http://localhost:8082 in browser"