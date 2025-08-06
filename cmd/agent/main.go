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

	"milvus-diagnostic-platform/pkg/analyzer"
	"milvus-diagnostic-platform/pkg/cleaner"
	"milvus-diagnostic-platform/pkg/collector"
	"milvus-diagnostic-platform/pkg/config"
	"milvus-diagnostic-platform/pkg/controller"
	"milvus-diagnostic-platform/pkg/dashboard"
	"milvus-diagnostic-platform/pkg/database"
	"milvus-diagnostic-platform/pkg/discovery"
	"milvus-diagnostic-platform/pkg/monitor"
	"milvus-diagnostic-platform/pkg/storage"
)

var (
	configPath   = flag.String("config", "/etc/agent/config.yaml", "Path to configuration file")
	kubeconfig   = flag.String("kubeconfig", "", "Path to kubeconfig file (optional, uses in-cluster config if not provided)")
	healthAddr   = flag.String("health-addr", ":8081", "Health check server address")
	metricsAddr  = flag.String("metrics-addr", ":8080", "Metrics server address")
	version      = "dev"
	buildTime    = "unknown"
	gitCommit    = "unknown"
)

func main() {
	flag.Parse()

	klog.Infof("Starting Milvus Coredump Agent")
	klog.Infof("Version: %s, Build Time: %s, Git Commit: %s", version, buildTime, gitCommit)

	cfg, err := config.Load(*configPath)
	if err != nil {
		klog.Fatalf("Failed to load configuration: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		klog.Fatalf("Invalid configuration: %v", err)
	}

	kubeClient, err := createKubernetesClient()
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	agent := &Agent{
		config:     cfg,
		kubeClient: kubeClient,
	}

	if err := agent.Run(ctx); err != nil {
		klog.Fatalf("Agent failed: %v", err)
	}

	klog.Info("Milvus Coredump Agent stopped")
}

type Agent struct {
	config     *config.Config
	kubeClient kubernetes.Interface
}

func (a *Agent) Run(ctx context.Context) error {
	klog.Info("Initializing agent components")

	// Initialize database
	dbConfig := &database.Config{
		Path:            a.config.Database.Path,
		MaxOpenConns:    a.config.Database.MaxOpenConns,
		MaxIdleConns:    a.config.Database.MaxIdleConns,
		ConnMaxLifetime: a.config.Database.ConnMaxLifetime,
	}
	
	db, err := database.New(dbConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer db.Close()

	// Initialize controller client if controller URL is configured
	var controllerClient *controller.Client
	if a.config.Controller.Enabled && a.config.Controller.URL != "" {
		controllerConfig := &controller.ClientConfig{
			ControllerURL:     a.config.Controller.URL,
			Timeout:          a.config.Controller.Timeout,
			HeartbeatInterval: a.config.Controller.HeartbeatInterval,
			NodeName:         a.config.Agent.NodeName,
			Version:          version,
		}
		controllerClient = controller.NewClient(controllerConfig)
		
		// Start controller client (begins heartbeat)
		go func() {
			if err := controllerClient.Start(ctx); err != nil {
				klog.Errorf("Controller client failed: %v", err)
			}
		}()
	} else {
		klog.Info("Controller client disabled or not configured")
	}

	discoveryManager := discovery.New(a.kubeClient, &a.config.Discovery)
	
	collectorManager := collector.New(&a.config.Collector, discoveryManager)
	
	analyzerManager := analyzer.New(&a.config.Analyzer, controllerClient)
	
	storageManager, err := storage.New(&a.config.Storage, &a.config.Analyzer)
	if err != nil {
		return fmt.Errorf("failed to create storage manager: %w", err)
	}
	
	cleanerManager := cleaner.New(&a.config.Cleaner, a.kubeClient, discoveryManager)
	
	var monitorManager *monitor.Monitor
	if a.config.Monitor.PrometheusEnabled {
		monitorManager = monitor.New(&a.config.Monitor)
	}

	// 创建Dashboard服务器
	var dashboardServer *dashboard.Server
	if a.config.Dashboard.Enabled {
		dashboardServer = dashboard.NewServer(
			&a.config.Dashboard,
			a.kubeClient,
			discoveryManager,
			collectorManager,
			analyzerManager,
			storageManager,
			cleanerManager,
			db,
		)
	}

	klog.Info("Starting health and metrics servers")
	go a.startHealthServer(ctx)
	if monitorManager != nil {
		go a.startMetricsServer(ctx, monitorManager)
	}

	klog.Info("Starting agent components")
	
	errChan := make(chan error, 6)

	if dashboardServer != nil {
		go func() {
			if err := dashboardServer.Start(ctx); err != nil {
				errChan <- fmt.Errorf("dashboard server failed: %w", err)
			}
		}()
	}

	go func() {
		if err := discoveryManager.Start(ctx); err != nil {
			errChan <- fmt.Errorf("discovery manager failed: %w", err)
		}
	}()

	go func() {
		if err := collectorManager.Start(ctx); err != nil {
			errChan <- fmt.Errorf("collector manager failed: %w", err)
		}
	}()

	go func() {
		collectorEvents := collectorManager.GetEventChannel()
		if err := analyzerManager.Start(ctx, collectorEvents); err != nil {
			errChan <- fmt.Errorf("analyzer manager failed: %w", err)
		}
	}()

	go func() {
		analyzerEvents := analyzerManager.GetEventChannel()
		if err := storageManager.Start(ctx, analyzerEvents); err != nil {
			errChan <- fmt.Errorf("storage manager failed: %w", err)
		}
	}()

	go func() {
		storageEvents := storageManager.GetEventChannel()
		if err := cleanerManager.Start(ctx, storageEvents); err != nil {
			errChan <- fmt.Errorf("cleaner manager failed: %w", err)
		}
	}()

	if monitorManager != nil {
		go func() {
			if err := monitorManager.Start(ctx, a.getMonitoringChannels(
				collectorManager, analyzerManager, storageManager, cleanerManager,
			)); err != nil {
				errChan <- fmt.Errorf("monitor manager failed: %w", err)
			}
		}()
	}

	klog.Info("All components started successfully")

	select {
	case <-ctx.Done():
		klog.Info("Shutdown signal received")
		return nil
	case err := <-errChan:
		return err
	}
}

func (a *Agent) startHealthServer(ctx context.Context) {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})
	
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"version":"%s","buildTime":"%s","gitCommit":"%s"}`, 
			version, buildTime, gitCommit)
	})

	server := &http.Server{
		Addr:    *healthAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	klog.Infof("Health server listening on %s", *healthAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Errorf("Health server failed: %v", err)
	}
}

func (a *Agent) startMetricsServer(ctx context.Context, monitorManager *monitor.Monitor) {
	server := &http.Server{
		Addr:    *metricsAddr,
		Handler: monitorManager.GetHandler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	klog.Infof("Metrics server listening on %s", *metricsAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Errorf("Metrics server failed: %v", err)
	}
}

func (a *Agent) getMonitoringChannels(
	collectorMgr *collector.Collector,
	analyzerMgr *analyzer.Analyzer,
	storageMgr *storage.Storage,
	cleanerMgr *cleaner.Cleaner,
) *monitor.Channels {
	return &monitor.Channels{
		CollectorEvents: collectorMgr.GetEventChannel(),
		AnalyzerEvents:  analyzerMgr.GetEventChannel(),
		StorageEvents:   storageMgr.GetEventChannel(),
		CleanerEvents:   cleanerMgr.GetEventChannel(),
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