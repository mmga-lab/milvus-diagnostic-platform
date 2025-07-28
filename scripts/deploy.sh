#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
NAMESPACE="${NAMESPACE:-default}"

echo "Deploying Milvus Coredump Agent to namespace: $NAMESPACE"

cd "$ROOT_DIR"

# Apply RBAC
echo "Creating RBAC resources..."
kubectl apply -f deployments/rbac.yaml

# Apply Secret (if API key is configured)
if [ -f deployments/secret.yaml ]; then
    echo "Creating Secrets..."
    kubectl apply -f deployments/secret.yaml
fi

# Apply ConfigMap
echo "Creating ConfigMap..."
kubectl apply -f deployments/configmap.yaml

# Apply DaemonSet
echo "Creating DaemonSet..."
kubectl apply -f deployments/daemonset.yaml

echo ""
echo "Deployment completed!"
echo ""
echo "Check the status with:"
echo "  kubectl get daemonset milvus-coredump-agent -n $NAMESPACE"
echo "  kubectl get pods -l app=milvus-coredump-agent -n $NAMESPACE"
echo ""
echo "View logs with:"
echo "  kubectl logs -l app=milvus-coredump-agent -n $NAMESPACE -f"
echo ""
echo "Check metrics (if port-forwarding):"
echo "  kubectl port-forward ds/milvus-coredump-agent 8080:8080 -n $NAMESPACE"
echo "  curl http://localhost:8080/metrics"