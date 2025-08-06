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
make docker-build

# Deploy to Kubernetes  
./scripts/deploy.sh
make deploy

# Build with custom image name/tag
IMAGE_NAME=my-agent IMAGE_TAG=v1.0.0 ./scripts/build.sh
IMAGE_NAME=my-agent IMAGE_TAG=v1.0.0 make docker-build
```

### Development Commands
```bash
# Run locally (requires kubeconfig)
go run cmd/agent/main.go --config=configs/config.yaml --kubeconfig=$HOME/.kube/config
make run

# Build binary only
go build -o milvus-coredump-agent cmd/agent/main.go
make build

# Install dependencies
go mod download
go mod tidy
make deps
make tidy
```

### Testing and Quality
```bash
# Run all tests
make test
go test -v -race ./...

# Run single test or package tests
go test -v ./pkg/analyzer/
go test -v -run TestAnalyzeCoredumpFile ./pkg/analyzer/

# Run tests with coverage
make test-coverage

# Run linter (requires golangci-lint)
make lint

# Format code
make fmt
gofmt -s -w .

# Pre-commit checks (fmt, lint, test)
make pre-commit

# Install development tools
make install-tools
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

# Access Web Dashboard
kubectl port-forward ds/milvus-coredump-agent 8082:8082
# Open http://localhost:8082 in browser

# Or use NodePort service
kubectl get svc milvus-coredump-agent-dashboard-nodeport
# Access via http://<node-ip>:30082
```

## Configuration System

Configuration is managed through `pkg/config/config.go` using Viper. The main config file is `configs/config.yaml`, deployed via ConfigMap. Key configuration sections:

- **Discovery**: Controls how Milvus instances are identified via labels (`helmReleaseLabels`, `operatorLabels`)
- **Analyzer**: Value scoring thresholds and GDB analysis settings
- **Cleaner**: Restart count thresholds (`maxRestartCount`) and cleanup timing
- **Storage**: Backend selection and retention policies
- **Dashboard**: Web dashboard configuration, viewer settings, and UI options

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

Files below the `valueThreshold` (configurable, default 4.0 for testing) are automatically skipped for storage.

### Detailed Scoring Logs
The system provides detailed Chinese language scoring breakdowns:
```log
分数计算详情 [/host/var/lib/systemd/coredump/core.milvus_crasher.1.xxx]: 
基础分: 4.0, 崩溃原因: +2.0, Panic关键词: +1.0, 栈跟踪质量: +1.5, Pod关联: +1.0 -> 总分: 9.50
```

## AI-Powered Analysis

The system integrates AI models for intelligent coredump analysis using RESTful API approach:

### Supported AI Providers
- **GLM (ChatGLM)**: Primary provider using `glm-4.5-flash` model via RESTful API
- **OpenAI**: GPT-4, GPT-3.5-turbo (removed SDK dependency for better compatibility)
- **Extensible**: Can add other providers through RESTful API interface

### AI Analysis Pipeline
1. **GDB Analysis First**: Traditional GDB analysis extracts technical details
2. **AI Context Building**: Creates structured prompt with stack trace, crash info, and context
3. **AI Model Analysis**: Sends context to AI API (GLM/OpenAI) for intelligent analysis
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

### AI Analysis Request Format
The system sends structured prompts to AI models:
```
COREDUMP ANALYSIS REQUEST
========================

Application: milvus_crasher
Signal: 11 (SIGSEGV)
PID: 12345
Kubernetes Pod: default/milvus-test
Milvus Instance: test-instance
Thread Count: 4

STACK TRACE:
```
(GDB stack trace with up to 3000 characters)
```

KEY REGISTERS:
rip = 0x12345678
rsp = 0x87654321
...

LOADED LIBRARIES:
- /lib/libc.so.6
- /usr/local/lib/libmilvus.so
...

Please analyze this coredump and provide structured debugging insights in JSON format.
```

### Cost Control Features
- Monthly spending limits (`maxCostPerMonth`)
- Hourly analysis limits (`maxAnalysisPerHour`)
- Smart token management with truncation
- Optional analysis for low-value coredumps

### GLM Configuration Example
```yaml
aiAnalysis:
  enabled: true
  provider: "glm"
  model: "glm-4.5-flash"
  apiKey: "your-glm-api-key"
  baseURL: "https://open.bigmodel.cn/api/paas/v4/chat/completions"
  timeout: "30s"
  maxTokens: 2000
  temperature: 0.3
  enableCostControl: true
  maxCostPerMonth: 100.0
  maxAnalysisPerHour: 50
```

### AI Analysis Implementation
- **RESTful API**: Uses direct HTTP client instead of vendor SDKs for better compatibility
- **Fixed Parameters**: Uses consistent temperature (0.3) and maxTokens (2000) for stable results
- **Error Handling**: Comprehensive logging and graceful degradation on API failures
- **Response Parsing**: Extracts JSON from AI responses and validates structure

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

## Web Dashboard

The system includes a comprehensive web dashboard for monitoring and managing coredump collection activities.

### Dashboard Features

**Main Overview Page**:
- Real-time statistics (instances discovered, coredumps processed, high-value files)  
- Processing trends chart showing coredump activity over time
- Value score distribution pie chart
- Recent activities feed

**Instance Management**:
- List all discovered Milvus instances with deployment type (Helm/Operator)
- Show instance status, pod counts, and associated coredump files
- View detailed pod information including restart counts and container status
- Filter coredumps by specific instance

**Coredump Analysis**:
- Comprehensive coredump file listing with sorting and filtering
- Detailed analysis results including GDB output and AI insights
- Value score breakdown showing how each file was evaluated
- One-click coredump viewer launcher

### Interactive Coredump Viewer

The dashboard provides a unique **Coredump Viewer** feature that launches temporary Kubernetes pods for interactive debugging:

**Viewer Capabilities**:
- Launches privileged debugging pods with GDB and analysis tools pre-installed
- Mounts coredump files from the host filesystem  
- Provides web-based terminal access via ttyd
- Automatically expires after configurable duration (default 30 minutes)
- Supports multiple concurrent viewers with resource limits

**Viewer Workflow**:
1. User clicks "启动查看器" (Launch Viewer) button on any stored coredump
2. System creates a temporary Kubernetes pod with debugging tools
3. Pod mounts the specific coredump file and host filesystem  
4. Web terminal interface is exposed via Service
5. User gets direct access to GDB and system debugging tools
6. Pod automatically cleans up after expiration time

### Dashboard Configuration

```yaml
dashboard:
  enabled: true
  port: 8082
  path: "/dashboard"  
  serveStatic: true
  staticPath: "/opt/milvus-coredump-agent/web/static"
  viewerNamespace: "default"
  viewer:
    enabled: true
    image: "ubuntu:22.04"
    defaultDuration: 30  # minutes
    maxDuration: 120     # minutes  
    maxConcurrentPods: 3
    coredumpPath: "/host/var/lib/systemd/coredump"
    webTerminalPort: 7681
```

### Dashboard Access Methods

**Development/Testing**:
```bash
kubectl port-forward ds/milvus-coredump-agent 8082:8082
# Access http://localhost:8082
```

**Production via NodePort**:
```bash
kubectl apply -f deployments/dashboard-service.yaml
# Access http://<node-ip>:30082  
```

**Production via Ingress**:
```bash
kubectl apply -f deployments/dashboard-ingress.yaml
# Configure DNS: milvus-dashboard.local -> cluster IP
# Access http://milvus-dashboard.local
```

## Testing and Debugging

### Coredump Generation for Testing

To test the agent functionality, you can create coredump files using a crash test program:

```yaml
# test-crash-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: milvus-test-crash
  labels:
    app.kubernetes.io/name: milvus
    helm.sh/chart: milvus
spec:
  containers:
  - name: milvus
    image: alpine:3.18
    command: ["/bin/sh"]
    args: ["-c", "ulimit -c unlimited && /crasher && sleep 30"]
    volumeMounts:
    - name: coredump-test-volume
      mountPath: /crasher
      subPath: milvus_crasher
    securityContext:
      allowPrivilegeEscalation: true
      capabilities:
        add: [SYS_PTRACE]
      runAsUser: 0
  volumes:
  - name: coredump-test-volume
    configMap:
      name: crash-test-program
      defaultMode: 0755
  restartPolicy: Always
```

### Prerequisites for Coredump Generation

1. **Host coredump configuration**:
   ```bash
   # Check core pattern
   cat /proc/sys/kernel/core_pattern
   
   # Should be: |/usr/lib/systemd/systemd-coredump %P %u %g %s %t %c %h
   
   # Check ulimit
   ulimit -c
   # Should be: unlimited
   ```

2. **systemd-coredump service**:
   ```bash
   systemctl status systemd-coredump.socket
   systemctl status systemd-coredump@*
   ```

### Common Issues and Solutions

#### 1. ConfigMap Path Mismatch
**Problem**: Agent can't find coredump files despite them existing on host
**Solution**: Ensure collector path matches DaemonSet mount:
```yaml
# In config.yaml
collector:
  coredumpPath: "/host/var/lib/systemd/coredump"  # Not /var/lib/systemd/coredump
```

#### 2. GLM API Parameter Errors
**Problem**: "API 调用参数有误，请检查文档" error from GLM API
**Solution**: Use fixed parameter values in AI analyzer:
```go
request := GLMChatRequest{
    Model: "glm-4.5-flash",
    Messages: messages,
    Temperature: 0.3,      // Fixed value
    MaxTokens:   2000,     // Fixed value
}
```

#### 3. No AI Analysis Triggered
**Problem**: AI analysis doesn't run despite coredump detection
**Possible Causes**:
- Value score below threshold (check `valueThreshold` in config)
- Cost control limits reached (`maxAnalysisPerHour`, `maxCostPerMonth`)
- API key not configured or invalid

### Monitoring Commands

```bash
# Monitor agent logs for scoring details
kubectl logs -l app=milvus-coredump-agent -f | grep "分数计算详情"

# Check AI analysis results
kubectl logs -l app=milvus-coredump-agent -f | grep "AI analysis completed"

# View GLM API interactions (debug logs)
kubectl logs -l app=milvus-coredump-agent -f | grep "GLM API"

# Check coredump file detection
kubectl logs -l app=milvus-coredump-agent -f | grep "Found coredump file"
```

### Verification Workflow

1. **Deploy test pod** → Generates coredump on crash
2. **Check discovery** → Agent detects Milvus instance and restart event
3. **Verify collection** → Agent finds coredump file in systemd directory
4. **Confirm GDB analysis** → Stack trace and crash info extracted
5. **Check value scoring** → Detailed scoring breakdown in Chinese
6. **Validate AI analysis** → GLM API integration and structured response
7. **Verify storage** → Coredump file stored according to retention policy

## Database & Persistence

The system uses SQLite for local persistence with a comprehensive schema supporting:

### Core Tables
- **milvus_instances**: Discovered Milvus deployments with metadata
- **pods**: Pod information linked to instances with restart tracking
- **coredump_files**: Complete coredump metadata and analysis results
- **analysis_results**: Detailed GDB analysis output (stack traces, crash info)
- **ai_analysis_results**: AI analysis results with cost tracking
- **restart_events**: Pod restart event history with panic detection
- **storage_events**: File storage operation tracking
- **cleanup_events**: Instance cleanup operation history

### Database Access
```bash
# View database content during development
sqlite3 data/coredump_agent.db ".tables"
sqlite3 data/coredump_agent.db "SELECT * FROM coredump_files ORDER BY created_at DESC LIMIT 5;"
sqlite3 data/coredump_agent.db "SELECT * FROM ai_analysis_results WHERE confidence > 0.8;"
```

## Grafana Dashboard

A pre-built Grafana dashboard is included at `dashboards/grafana-dashboard.json` with panels for:
- Coredump discovery and processing metrics
- Value score distributions
- AI analysis cost tracking
- Instance and cleanup statistics

## Recent Improvements

### Database Integration (Latest)
- **SQLite Persistence**: Added comprehensive database schema for all operations
- **Data Analytics**: Enhanced dashboard with historical data and trend analysis
- **Cost Tracking**: Detailed AI analysis cost tracking per request and monthly limits

### GLM AI Integration  
- **RESTful API Implementation**: Replaced OpenAI SDK with direct HTTP client for better compatibility
- **GLM-4.5-Flash Support**: Integrated ChatGLM as primary AI provider
- **Parameter Debugging**: Fixed GLM API parameter format issues with consistent temperature/maxTokens values
- **Comprehensive Logging**: Added detailed request/response logging for AI API interactions

### Enhanced Value Scoring System
- **Configurable Threshold**: Made `valueThreshold` configurable (lowered to 4.0 for testing)
- **Chinese Language Logs**: Added detailed scoring breakdowns in Chinese for better debugging
- **Multi-dimensional Scoring**: Expanded scoring algorithm with 8 different dimensions

### Path Configuration Fixes
- **Collector Path Correction**: Fixed coredump path from `/var/lib/systemd/coredump` to `/host/var/lib/systemd/coredump`
- **ConfigMap Mount Alignment**: Ensured DaemonSet volume mounts match collector configuration

### Testing Infrastructure
- **Pre-built Crash Program**: Created `Dockerfile.crasher` with compiled crash test binary
- **Automated Testing**: Developed systematic testing approach for complete workflow verification
- **Real Coredump Generation**: Moved from simulated to actual binary coredump files for GDB compatibility

## Test Files

Unit tests are located alongside their respective packages:
- `pkg/analyzer/analyzer_basic_test.go` - Analyzer component tests
- `pkg/collector/collector_basic_test.go` - Collector component tests
- `pkg/discovery/discovery_basic_test.go` - Discovery component tests
- `pkg/config/config_basic_test.go` - Configuration tests

## Makefile Targets

Key make targets for development:
- `make all` - Run lint, test, and build
- `make build` - Build the binary with proper version info
- `make test` - Run tests with race detection
- `make test-coverage` - Generate HTML coverage report
- `make lint` - Run golangci-lint or go vet
- `make fmt` - Format all Go code
- `make docker-build` - Build Docker image with version metadata
- `make docker-push` - Build and push Docker image
- `make deploy` - Deploy to Kubernetes cluster
- `make run` - Run locally with kubeconfig
- `make pre-commit` - Run all checks before committing