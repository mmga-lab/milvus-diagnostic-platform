#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Configuration
NAMESPACE="${NAMESPACE:-default}"
RELEASE_NAME="${RELEASE_NAME:-milvus-coredump-agent}"
CHART_PATH="${ROOT_DIR}/helm/milvus-coredump-agent"
VALUES_FILE="${VALUES_FILE:-}"
GLM_API_KEY="${GLM_API_KEY:-}"

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
    echo "  -f, --values FILE            Values file path"
    echo "  -k, --api-key KEY            GLM API key"
    echo "  --dry-run                    Perform a dry run"
    echo "  --upgrade                    Upgrade existing release"
    echo "  -h, --help                   Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  NAMESPACE                    Kubernetes namespace"
    echo "  RELEASE_NAME                 Helm release name"  
    echo "  VALUES_FILE                  Values file path"
    echo "  GLM_API_KEY                  GLM API key"
    echo ""
    echo "Examples:"
    echo "  $0 --namespace milvus-system"
    echo "  $0 --values prod-values.yaml --api-key sk-xxx"
    echo "  $0 --dry-run"
}

# Parse command line arguments
DRY_RUN=false
UPGRADE=false

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
        -f|--values)
            VALUES_FILE="$2"
            shift 2
            ;;
        -k|--api-key)
            GLM_API_KEY="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --upgrade)
            UPGRADE=true
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

print_step "Deploying Milvus Coredump Agent with Helm"
echo "Namespace: $NAMESPACE"
echo "Release: $RELEASE_NAME"
echo "Chart: $CHART_PATH"
if [[ -n "$VALUES_FILE" ]]; then
    echo "Values file: $VALUES_FILE"
fi
echo ""

# Check prerequisites
print_step "Checking prerequisites..."

if ! command -v helm &> /dev/null; then
    print_error "Helm is not installed. Please install Helm first."
    exit 1
fi

if ! command -v kubectl &> /dev/null; then
    print_error "kubectl is not installed. Please install kubectl first."
    exit 1
fi

# Test kubectl connectivity
if ! kubectl cluster-info &> /dev/null; then
    print_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

print_success "Prerequisites check passed"

# Create namespace if it doesn't exist
print_step "Creating namespace if needed..."
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    kubectl create namespace "$NAMESPACE"
    print_success "Created namespace: $NAMESPACE"
else
    print_success "Namespace already exists: $NAMESPACE"
fi

# Create secret for GLM API key if provided
if [[ -n "$GLM_API_KEY" ]]; then
    print_step "Creating GLM API key secret..."
    kubectl create secret generic milvus-coredump-secrets \
        --from-literal=glm-api-key="$GLM_API_KEY" \
        --namespace="$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    print_success "GLM API key secret created/updated"
fi

# Prepare Helm command
HELM_CMD="helm"
if [[ "$UPGRADE" == "true" ]]; then
    HELM_CMD="$HELM_CMD upgrade"
else
    HELM_CMD="$HELM_CMD install"
fi

HELM_CMD="$HELM_CMD $RELEASE_NAME $CHART_PATH"
HELM_CMD="$HELM_CMD --namespace $NAMESPACE"
HELM_CMD="$HELM_CMD --create-namespace"

if [[ -n "$VALUES_FILE" ]]; then
    if [[ ! -f "$VALUES_FILE" ]]; then
        print_error "Values file not found: $VALUES_FILE"
        exit 1
    fi
    HELM_CMD="$HELM_CMD --values $VALUES_FILE"
fi

if [[ "$DRY_RUN" == "true" ]]; then
    HELM_CMD="$HELM_CMD --dry-run"
fi

# Execute Helm command
print_step "Executing Helm deployment..."
echo "Command: $HELM_CMD"
echo ""

if eval "$HELM_CMD"; then
    if [[ "$DRY_RUN" == "false" ]]; then
        print_success "Deployment completed successfully!"
        echo ""
        
        print_step "Deployment status:"
        helm status "$RELEASE_NAME" --namespace "$NAMESPACE"
        
        echo ""
        print_step "Useful commands:"
        echo "  # Check controller status:"
        echo "  kubectl get deployment ${RELEASE_NAME}-controller -n $NAMESPACE"
        echo ""
        echo "  # Check agent status:"
        echo "  kubectl get daemonset ${RELEASE_NAME}-agent -n $NAMESPACE"
        echo ""
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
        echo "  # Access dashboard (if enabled):"
        echo "  kubectl port-forward ds/${RELEASE_NAME}-agent 8082:8082 -n $NAMESPACE"
        echo "  # Open http://localhost:8082 in browser"
    else
        print_success "Dry run completed successfully!"
    fi
else
    print_error "Deployment failed!"
    exit 1
fi