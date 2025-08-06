# Legacy Deployment Files

⚠️ **These files are deprecated and kept for reference only.**

## Recommended Approach

Please use the Helm chart for deployments:

```bash
# Install with Helm
./scripts/helm-install.sh

# Or using Make
make helm-install-dev    # Development
make helm-install-prod   # Production
```

## Contents

This directory contains the original Kubernetes manifests that have been superseded by the Helm chart:

- `controller-*.yaml` - Original controller deployment files
- These files are no longer maintained and may be outdated

## Migration

If you were using these files directly, please migrate to the Helm chart:

1. Uninstall the old deployment:
   ```bash
   kubectl delete -f deployments/legacy/
   ```

2. Install using Helm:
   ```bash
   ./scripts/helm-install.sh
   ```

For more information, see the [Helm chart documentation](../helm/milvus-coredump-agent/README.md).