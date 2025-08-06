# Milvus Coredump Agent Helm Chart

This Helm chart deploys the Milvus Coredump Agent with Controller architecture on a Kubernetes cluster.

## Architecture

The Milvus Coredump Agent consists of two main components:

- **Controller**: A centralized coordinator that manages AI analysis cost control, instance cleanup decisions, and global state
- **Agent (DaemonSet)**: Runs on each node to collect, analyze, and process coredump files

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+
- Persistent storage (if persistence is enabled)

## Installation

### Quick Start

1. **Add the repository** (if available):
```bash
helm repo add milvus-coredump-agent https://your-repo.com/charts
helm repo update
```

2. **Install with default values**:
```bash
helm install milvus-coredump-agent milvus-coredump-agent/milvus-coredump-agent
```

### Local Installation

1. **Clone the repository**:
```bash
git clone <repository-url>
cd helm/milvus-coredump-agent
```

2. **Install from local chart**:
```bash
helm install milvus-coredump-agent . --namespace milvus-system --create-namespace
```

### Using the Installation Script

The repository includes a convenient installation script:

```bash
# Basic installation
./scripts/helm-install.sh

# Development configuration
./scripts/helm-install.sh --values examples/development-values.yaml

# Production configuration  
./scripts/helm-install.sh --values examples/production-values.yaml --namespace milvus-system

# With GLM API key
./scripts/helm-install.sh --api-key sk-your-glm-api-key
```

## Configuration

### GLM API Key Setup

The system requires a GLM (ChatGLM) API key for AI analysis. Set it up using one of these methods:

1. **Using installation script**:
```bash
./scripts/helm-install.sh --api-key sk-your-glm-api-key
```

2. **Manual secret creation**:
```bash
kubectl create secret generic milvus-coredump-secrets \
  --from-literal=glm-api-key=sk-your-glm-api-key \
  --namespace milvus-system
```

3. **Using values file**:
```yaml
external:
  glmApiKey:
    secret: milvus-coredump-secrets
    key: glm-api-key
```

### Key Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.enabled` | Enable controller deployment | `true` |
| `controller.persistence.enabled` | Enable persistent storage for controller | `true` |
| `controller.config.aiAnalysis.maxCostPerMonth` | Monthly AI analysis cost limit (USD) | `100.0` |
| `agent.enabled` | Enable agent daemonset | `true` |
| `agent.hostNetwork` | Use host network for agent | `true` |
| `agent.config.analyzer.valueThreshold` | Coredump value score threshold | `4.0` |
| `dashboard.enabled` | Enable web dashboard | `true` |
| `monitoring.prometheus.enabled` | Enable Prometheus metrics | `true` |

### Example Configurations

#### Development Environment
```yaml
# values-dev.yaml
controller:
  persistence:
    enabled: false
  config:
    aiAnalysis:
      maxCostPerMonth: 20.0
    cleaner:
      enabled: false

agent:
  config:
    logLevel: debug
    analyzer:
      valueThreshold: 2.0
```

#### Production Environment
```yaml
# values-prod.yaml
controller:
  replicaCount: 1
  persistence:
    enabled: true
    size: 50Gi
    storageClass: fast-ssd
  config:
    aiAnalysis:
      maxCostPerMonth: 500.0
    cleaner:
      enabled: true
      maxRestartCount: 3

agent:
  resources:
    limits:
      cpu: 2000m
      memory: 2Gi
  config:
    analyzer:
      valueThreshold: 6.0
    storage:
      maxStorageSize: 100GB
```

## Monitoring

### Prometheus Metrics

The chart exposes Prometheus metrics on both controller and agent components:

- Controller metrics: `http://controller:8091/metrics`
- Agent metrics: `http://agent:8080/metrics`

### ServiceMonitor

Enable ServiceMonitor for automatic Prometheus discovery:

```yaml
monitoring:
  prometheus:
    servicemonitor:
      enabled: true
      namespace: monitoring
```

### Dashboard Access

Access the web dashboard for monitoring and coredump management:

```bash
kubectl port-forward ds/milvus-coredump-agent-agent 8082:8082
# Open http://localhost:8082 in browser
```

## Upgrading

### Using Helm

```bash
helm upgrade milvus-coredump-agent . --namespace milvus-system
```

### Using the Script

```bash
./scripts/helm-install.sh --upgrade --namespace milvus-system
```

## Uninstallation

### Using Helm

```bash
helm uninstall milvus-coredump-agent --namespace milvus-system
```

### Using the Script

```bash
# Remove everything including data
./scripts/helm-uninstall.sh --namespace milvus-system

# Keep persistent data
./scripts/helm-uninstall.sh --keep-data --namespace milvus-system
```

## Troubleshooting

### Common Issues

1. **Controller not starting**:
   ```bash
   kubectl describe deployment milvus-coredump-agent-controller
   kubectl logs -l app.kubernetes.io/component=controller
   ```

2. **Agent pods not running**:
   ```bash
   kubectl describe daemonset milvus-coredump-agent-agent
   kubectl logs -l app.kubernetes.io/component=agent
   ```

3. **Permission errors**:
   - Ensure the ServiceAccount has proper RBAC permissions
   - Check if the agent has required host access capabilities

4. **AI analysis not working**:
   - Verify GLM API key is correctly configured
   - Check controller logs for API errors
   - Ensure cost limits haven't been exceeded

### Useful Commands

```bash
# Check controller API status
kubectl port-forward svc/milvus-coredump-agent-controller 8090:8090
curl http://localhost:8090/api/stats

# View detailed status
helm status milvus-coredump-agent

# Generate debug information
kubectl describe deployment milvus-coredump-agent-controller
kubectl describe daemonset milvus-coredump-agent-agent
kubectl get events --sort-by=.metadata.creationTimestamp
```

## Values Reference

See [values.yaml](values.yaml) for the complete list of configurable parameters.

## Examples

- [Development configuration](examples/development-values.yaml)
- [Production configuration](examples/production-values.yaml)