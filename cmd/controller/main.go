package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"milvus-diagnostic-platform/pkg/config"
	"milvus-diagnostic-platform/pkg/controller"
	"milvus-diagnostic-platform/pkg/database"
)

var (
	configPath   = flag.String("config", "/etc/controller/config.yaml", "Path to configuration file")
	kubeconfig   = flag.String("kubeconfig", "", "Path to kubeconfig file (optional)")
	httpAddr     = flag.String("http-addr", ":8090", "HTTP API server address")
	metricsAddr  = flag.String("metrics-addr", ":8091", "Metrics server address")
	version      = "dev"
	buildTime    = "unknown"
	gitCommit    = "unknown"
)

func main() {
	flag.Parse()

	klog.Infof("Starting Milvus Coredump Controller")
	klog.Infof("Version: %s, Build Time: %s, Git Commit: %s", version, buildTime, gitCommit)

	cfg, err := config.Load(*configPath)
	if err != nil {
		klog.Fatalf("Failed to load configuration: %v", err)
	}

	kubeClient, err := createKubernetesClient()
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize database for global state
	dbConfig := &database.Config{
		Path:            cfg.Database.Path,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	}
	
	db, err := database.New(dbConfig)
	if err != nil {
		klog.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create controller manager
	controllerManager := controller.NewManager(&controller.Config{
		Config:     cfg,
		KubeClient: kubeClient,
		Database:   db,
	})

	// Start HTTP API server
	go startHTTPServer(ctx, controllerManager, *httpAddr)

	// Start metrics server  
	go startMetricsServer(ctx, controllerManager, *metricsAddr)

	// Start controller manager
	if err := controllerManager.Start(ctx); err != nil {
		klog.Fatalf("Controller manager failed: %v", err)
	}

	klog.Info("Milvus Coredump Controller stopped")
}

func startHTTPServer(ctx context.Context, manager *controller.Manager, addr string) {
	mux := http.NewServeMux()
	
	// API endpoints
	mux.HandleFunc("/api/ai-analysis/request", manager.HandleAIAnalysisRequest)
	mux.HandleFunc("/api/cleanup/request", manager.HandleCleanupRequest)
	mux.HandleFunc("/api/stats", manager.HandleGetStats)
	mux.HandleFunc("/api/heartbeat", manager.HandleHeartbeat)
	
	// Health endpoints
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	klog.Infof("HTTP API server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Errorf("HTTP server failed: %v", err)
	}
}

func startMetricsServer(ctx context.Context, manager *controller.Manager, addr string) {
	server := &http.Server{
		Addr:    addr,
		Handler: manager.GetMetricsHandler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	klog.Infof("Metrics server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Errorf("Metrics server failed: %v", err)
	}
}

func createKubernetesClient() (kubernetes.Interface, error) {
	var kubeConfig *rest.Config
	var err error

	if *kubeconfig != "" {
		klog.Infof("Using kubeconfig from file: %s", *kubeconfig)
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		klog.Info("Using in-cluster configuration")
		kubeConfig, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubeconfig: %w", err)
	}

	kubeConfig.QPS = 50
	kubeConfig.Burst = 100

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return client, nil
}