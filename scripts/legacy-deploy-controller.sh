#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
NAMESPACE="${NAMESPACE:-default}"

echo "Deploying Milvus Coredump Controller to namespace: $NAMESPACE"

cd "$ROOT_DIR"

# Deploy controller configuration
echo "Creating controller ConfigMap..."
kubectl apply -f deployments/controller-configmap.yaml

# Deploy controller
echo "Creating controller Deployment..."
kubectl apply -f deployments/controller-deployment.yaml

echo ""
echo "Controller deployment completed!"
echo ""
echo "Check the status with:"
echo "  kubectl get deployment milvus-coredump-controller -n $NAMESPACE"
echo "  kubectl get pods -l app=milvus-coredump-controller -n $NAMESPACE"
echo ""
echo "View logs with:"
echo "  kubectl logs -l app=milvus-coredump-controller -n $NAMESPACE -f"
echo ""
echo "Access API (if port-forwarding):"
echo "  kubectl port-forward deployment/milvus-coredump-controller 8090:8090 -n $NAMESPACE"
echo "  curl http://localhost:8090/api/stats"