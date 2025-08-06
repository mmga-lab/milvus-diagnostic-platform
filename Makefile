.PHONY: all build test lint fmt clean docker-build docker-push deploy

# Variables
AGENT_BINARY_NAME := milvus-diagnostic-agent
CONTROLLER_BINARY_NAME := milvus-diagnostic-controller
IMAGE_NAME ?= milvus-diagnostic-platform
IMAGE_TAG ?= latest
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 'dev')
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo 'unknown')
LDFLAGS := -ldflags="-w -s -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOFMT := gofmt
GOVET := $(GOCMD) vet
GOLINT := golangci-lint

# Directories
AGENT_CMD_DIR := ./cmd/agent
CONTROLLER_CMD_DIR := ./cmd/controller
PKG_DIR := ./pkg/...
ALL_PACKAGES := ./...

all: lint test build

# Build both binaries
build: build-agent build-controller

# Build the agent binary
build-agent:
	@echo "Building $(AGENT_BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(AGENT_BINARY_NAME) $(AGENT_CMD_DIR)
	@echo "Build complete: $(AGENT_BINARY_NAME)"

# Build the controller binary
build-controller:
	@echo "Building $(CONTROLLER_BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(CONTROLLER_BINARY_NAME) $(CONTROLLER_CMD_DIR)
	@echo "Build complete: $(CONTROLLER_BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out $(ALL_PACKAGES)
	@echo "Tests complete"

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) > /dev/null 2>&1; then \
		$(GOLINT) run $(ALL_PACKAGES); \
	else \
		echo "golangci-lint not found, running go vet instead"; \
		$(GOVET) $(ALL_PACKAGES); \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .
	@echo "Code formatted"

# Run go mod tidy
tidy:
	@echo "Running go mod tidy..."
	$(GOCMD) mod tidy
	@echo "go.mod and go.sum updated"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(AGENT_BINARY_NAME) $(CONTROLLER_BINARY_NAME)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) .
	@echo "Docker build complete: $(IMAGE_NAME):$(IMAGE_TAG)"

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image..."
	docker push $(IMAGE_NAME):$(IMAGE_TAG)
	@echo "Docker push complete"

# Deploy to Kubernetes
deploy:
	@echo "Deploying to Kubernetes..."
	./scripts/deploy.sh
	@echo "Deployment complete"

# Run locally (requires kubeconfig)
run: build
	@echo "Running locally..."
	./$(BINARY_NAME) --config=configs/config.yaml --kubeconfig=$(HOME)/.kube/config

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GOGET) -u ./...
	$(GOCMD) mod download
	@echo "Dependencies installed"

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed"

# Check code before commit
pre-commit: fmt lint test
	@echo "Pre-commit checks passed!"

# Helm operations
helm-lint:
	@echo "Linting Helm chart..."
	helm lint helm/milvus-diagnostic-platform
	@echo "Helm chart lint complete"

helm-template:
	@echo "Generating Helm templates..."
	helm template milvus-diagnostic-platform helm/milvus-diagnostic-platform --output-dir generated/
	@echo "Templates generated in generated/ directory"

helm-install:
	@echo "Installing with Helm..."
	./scripts/helm-install.sh

helm-install-dev:
	@echo "Installing development configuration with Helm..."
	./scripts/helm-install.sh --values helm/milvus-diagnostic-platform/examples/development-values.yaml

helm-install-prod:
	@echo "Installing production configuration with Helm..."
	./scripts/helm-install.sh --values helm/milvus-diagnostic-platform/examples/production-values.yaml

helm-uninstall:
	@echo "Uninstalling with Helm..."
	./scripts/helm-uninstall.sh

helm-package:
	@echo "Packaging Helm chart..."
	helm package helm/milvus-diagnostic-platform
	@echo "Chart packaged"

# Show help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build targets:"
	@echo "  make build          - Build both agent and controller binaries"
	@echo "  make build-agent    - Build the agent binary"
	@echo "  make build-controller - Build the controller binary"
	@echo "  make docker-build   - Build Docker image with both binaries"
	@echo "  make docker-push    - Build and push Docker image"
	@echo ""
	@echo "Development targets:"
	@echo "  make test           - Run tests"
	@echo "  make test-coverage  - Run tests with coverage report"
	@echo "  make lint           - Run linter"
	@echo "  make fmt            - Format code"
	@echo "  make tidy           - Run go mod tidy"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make pre-commit     - Run all checks before commit"
	@echo ""
	@echo "Deployment targets:"
	@echo "  make deploy         - Deploy to Kubernetes (legacy)"
	@echo "  make run            - Run locally with kubeconfig"
	@echo ""
	@echo "Helm targets:"
	@echo "  make helm-lint      - Lint Helm chart"
	@echo "  make helm-template  - Generate Helm templates"
	@echo "  make helm-install   - Install with default values"
	@echo "  make helm-install-dev - Install with development values"
	@echo "  make helm-install-prod - Install with production values"
	@echo "  make helm-uninstall - Uninstall Helm release"
	@echo "  make helm-package   - Package Helm chart"
	@echo ""
	@echo "Utility targets:"
	@echo "  make deps           - Install dependencies"
	@echo "  make install-tools  - Install development tools"
	@echo "  make help           - Show this help message"