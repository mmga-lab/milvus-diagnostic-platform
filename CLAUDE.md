# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Kubernetes DaemonSet agent for automatically collecting and analyzing Milvus instance coredump files. The agent runs on each cluster node and intelligently manages coredump files from crashed Milvus instances, filtering valuable debugging information and automatically cleaning up problematic deployments.

## Architecture & Event-Driven Design

The system follows an event-driven architecture with six main components communicating through channels:

1. **Discovery** (`pkg/discovery/`) - Discovers Milvus instances (Helm/Operator deployments) and monitors Pod restart events
2. **Collector** (`pkg/collector/`) - Monitors filesystem for coredump files and correlates them with restart events 
3. **Analyzer** (`pkg/analyzer/`) - Analyzes coredump files using GDB, extracts crash information, and calculates value scores (0-10)
4. **Storage** (`pkg/storage/storage.go`) - Manages storage backends (local/S3/NFS), handles compression, and enforces retention policies
5. **Cleaner** (`pkg/cleaner/`) - Tracks restart counts and automatically uninstalls problematic Milvus instances using Helm/kubectl
6. **Monitor** (`pkg/monitor/`) - Provides Prometheus metrics and health endpoints

**Data Flow**: RestartEvent → CoredumpFile → AnalysisResults → StorageEvent → CleanupEvent

## Core Data Types

Key types that flow through the system:
- `discovery.MilvusInstance` - Represents discovered Milvus deployments with type (helm/operator)
- `collector.CoredumpFile` - Coredump file metadata with analysis results and value score
- `discovery.RestartEvent` - Pod restart information with panic detection (`IsPanic` field)
- `analyzer.AnalysisResults` - GDB analysis output including stack trace and crash reason

## Common Commands

### Build and Deploy
```bash
# Build Docker image
./scripts/build.sh

# Deploy to Kubernetes  
./scripts/deploy.sh

# Build with custom image name/tag
IMAGE_NAME=my-agent IMAGE_TAG=v1.0.0 ./scripts/build.sh
```

### Development Commands
```bash
# Run locally (requires kubeconfig)
go run cmd/agent/main.go --config=configs/config.yaml --kubeconfig=$HOME/.kube/config

# Build binary only
go build -o milvus-coredump-agent cmd/agent/main.go

# Install dependencies
go mod download
go mod tidy
```

### Monitoring and Debugging
```bash
# Check DaemonSet status
kubectl get daemonset milvus-coredump-agent

# View logs with different verbosity
kubectl logs -l app=milvus-coredump-agent -f
kubectl logs -l app=milvus-coredump-agent -f --previous

# Access metrics
kubectl port-forward ds/milvus-coredump-agent 8080:8080
curl http://localhost:8080/metrics

# Health checks
kubectl port-forward ds/milvus-coredump-agent 8081:8081  
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
```

## Configuration System

Configuration is managed through `pkg/config/config.go` using Viper. The main config file is `configs/config.yaml`, deployed via ConfigMap. Key configuration sections:

- **Discovery**: Controls how Milvus instances are identified via labels (`helmReleaseLabels`, `operatorLabels`)
- **Analyzer**: Value scoring thresholds and GDB analysis settings
- **Cleaner**: Restart count thresholds (`maxRestartCount`) and cleanup timing
- **Storage**: Backend selection and retention policies

## Channel-Based Communication

The agent uses Go channels for component communication. Each component exposes an event channel that others can subscribe to:

```go
// In main.go - typical channel wiring
collectorEvents := collectorManager.GetEventChannel()
analyzerManager.Start(ctx, collectorEvents)

analyzerEvents := analyzerManager.GetEventChannel()  
storageManager.Start(ctx, analyzerEvents)
```

## Milvus Instance Detection

The discovery system identifies Milvus instances through Kubernetes labels:

**Helm deployments**: 
- `app.kubernetes.io/name=milvus`
- `helm.sh/chart=milvus`

**Operator deployments**:
- `app.kubernetes.io/managed-by=milvus-operator`
- `milvus.io/instance`

## Value Scoring Algorithm

The analyzer calculates coredump value scores (0-10) using a **rule-based system only**. AI analysis does NOT participate in scoring but is applied to files that pass the threshold.

### Scoring Dimensions
- **Base score**: 4.0 points
- **Crash reason clarity**: +2.0 (clear crash reason identified)
- **Panic keywords**: +1.0 (contains panic/fatal/SIGSEGV etc.)
- **Stack trace quality**: +1.5 (>100 characters of stack trace)
- **Multi-thread complexity**: +0.5 (thread count > 1)
- **Pod association**: +1.0 (linked to specific Pod/instance)
- **Signal severity**: +1.0 (SIGSEGV=11, SIGABRT=6, SIGFPE=8)
- **File size**: +0.5 (>100MB, contains more info)
- **Freshness**: +0.5 (<1 hour old)

### Scoring Flow
```
GDB Analysis → Rule-based Scoring → Threshold Check → Storage Decision → Optional AI Analysis
```

Files below the `valueThreshold` (default 7.0) are automatically skipped for storage.

## AI-Powered Analysis

The system integrates OpenAI GPT-4 for intelligent coredump analysis:

### AI Analysis Pipeline
1. **GDB Analysis First**: Traditional GDB analysis extracts technical details
2. **AI Context Building**: Creates structured prompt with stack trace, crash info, and context
3. **GPT-4 Analysis**: Sends context to OpenAI API for intelligent analysis
4. **Structured Results**: Parses JSON response into structured debugging insights

### AI Analysis Results Structure
```go
type AIAnalysisResult struct {
    Summary          string              // Brief crash summary
    RootCause        string              // Most likely root cause
    Impact           string              // Impact assessment
    Recommendations  []string            // Actionable recommendations
    Confidence       float64             // AI confidence (0-1)
    CodeSuggestions  []CodeSuggestion    // Specific code fixes
    RelatedIssues    []string            // Known similar issues
}
```

### Cost Control Features
- Monthly spending limits (`maxCostPerMonth`)
- Hourly analysis limits (`maxAnalysisPerHour`)
- Smart token management with truncation
- Optional analysis for low-value coredumps

### Configuration
AI analysis is configured via `config.AIAnalysisConfig`:
- Provider support: OpenAI, Azure OpenAI, Anthropic (extensible)
- Model selection: GPT-4, GPT-3.5-turbo
- API key management via environment variables or config
- Cost control parameters

## Automatic Cleanup Logic

The cleaner tracks restart counts per instance within a time window. When `maxRestartCount` is exceeded:
1. Instance is marked for cleanup after `cleanupDelay`
2. Helm releases are uninstalled via `helm uninstall`
3. Operator instances are deleted via Kubernetes API calls
4. This prevents infinite coredump generation loops

## Storage Backends

Storage system supports multiple backends via interface:
- **Local**: Filesystem storage with directory organization by instance
- **S3**: AWS S3 compatible storage (placeholder implementation)
- **NFS**: Network filesystem storage (placeholder implementation)

Files are stored with naming: `{timestamp}_{podName}_{containerName}.core.gz`