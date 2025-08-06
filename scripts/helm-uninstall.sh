#!/bin/bash

set -e

# Configuration
NAMESPACE="${NAMESPACE:-default}"
RELEASE_NAME="${RELEASE_NAME:-milvus-coredump-agent}"

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
    echo "  --keep-data                  Keep persistent data (PVC and secrets)"
    echo "  -h, --help                   Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  NAMESPACE                    Kubernetes namespace"
    echo "  RELEASE_NAME                 Helm release name"
}

# Parse command line arguments
KEEP_DATA=false

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
        --keep-data)
            KEEP_DATA=true
            shift
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

print_step "Uninstalling Milvus Coredump Agent"
echo "Namespace: $NAMESPACE"
echo "Release: $RELEASE_NAME"
echo "Keep data: $KEEP_DATA"
echo ""

# Check if release exists
if ! helm list -n "$NAMESPACE" | grep -q "$RELEASE_NAME"; then
    print_warning "Release $RELEASE_NAME not found in namespace $NAMESPACE"
    exit 0
fi

# Uninstall Helm release
print_step "Uninstalling Helm release..."
if helm uninstall "$RELEASE_NAME" --namespace "$NAMESPACE"; then
    print_success "Helm release uninstalled successfully"
else
    print_error "Failed to uninstall Helm release"
    exit 1
fi

# Clean up additional resources if not keeping data
if [[ "$KEEP_DATA" == "false" ]]; then
    print_step "Cleaning up persistent resources..."
    
    # Remove PVC
    if kubectl get pvc "${RELEASE_NAME}-controller-data" -n "$NAMESPACE" &> /dev/null; then
        kubectl delete pvc "${RELEASE_NAME}-controller-data" -n "$NAMESPACE"
        print_success "Removed controller PVC"
    fi
    
    # Remove secrets
    if kubectl get secret milvus-coredump-secrets -n "$NAMESPACE" &> /dev/null; then
        kubectl delete secret milvus-coredump-secrets -n "$NAMESPACE"
        print_success "Removed API key secret"
    fi
    
    # Remove host data directories (optional - commented out for safety)
    print_warning "Host data directories at /var/lib/milvus-coredump-agent are preserved"
    print_warning "Remove them manually if needed: sudo rm -rf /var/lib/milvus-coredump-agent"
else
    print_warning "Keeping persistent data (PVC and secrets)"
fi

print_success "Uninstall completed!"
echo ""
print_step "To completely clean up, you may also want to:"
echo "  # Remove namespace (if empty):"
echo "  kubectl delete namespace $NAMESPACE"
echo ""
echo "  # Remove host data directories on each node:"
echo "  sudo rm -rf /var/lib/milvus-coredump-agent"