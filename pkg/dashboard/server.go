package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"milvus-diagnostic-platform/pkg/analyzer"
	"milvus-diagnostic-platform/pkg/cleaner"
	"milvus-diagnostic-platform/pkg/collector"
	"milvus-diagnostic-platform/pkg/config"
	"milvus-diagnostic-platform/pkg/database"
	"milvus-diagnostic-platform/pkg/discovery"
	"milvus-diagnostic-platform/pkg/storage"
)

// Server Dashboard Web服务器
type Server struct {
	config        *config.DashboardConfig
	kubeClient    kubernetes.Interface
	
	aggregator    *DataAggregator
	viewer        *CoredumpViewer
	handlers      *Handlers
	
	httpServer    *http.Server
}

// NewServer 创建新的Dashboard服务器
func NewServer(
	cfg *config.DashboardConfig,
	kubeClient kubernetes.Interface,
	discoveryMgr *discovery.Discovery,
	collectorMgr *collector.Collector,
	analyzerMgr *analyzer.Analyzer,
	storageMgr *storage.Storage,
	cleanerMgr *cleaner.Cleaner,
	db *database.Database,
) *Server {
	// 创建数据聚合器
	aggregator := NewDataAggregator(
		discoveryMgr,
		collectorMgr,
		analyzerMgr,
		storageMgr,
		cleanerMgr,
		db,
	)

	// 创建coredump查看器
	viewerConfig := ViewerConfig{
		Enabled:           cfg.Viewer.Enabled,
		Image:             cfg.Viewer.Image,
		ImagePullPolicy:   cfg.Viewer.ImagePullPolicy,
		DefaultDuration:   cfg.Viewer.DefaultDuration,
		MaxDuration:       cfg.Viewer.MaxDuration,
		MaxConcurrentPods: cfg.Viewer.MaxConcurrentPods,
		CoredumpPath:      cfg.Viewer.CoredumpPath,
		WebTerminalPort:   cfg.Viewer.WebTerminalPort,
	}
	
	viewer := NewCoredumpViewer(
		kubeClient,
		cfg.ViewerNamespace,
		viewerConfig,
	)

	// 创建API处理器
	handlers := NewHandlers(aggregator, viewer)

	return &Server{
		config:     cfg,
		kubeClient: kubeClient,
		aggregator: aggregator,
		viewer:     viewer,
		handlers:   handlers,
	}
}

// Start 启动Dashboard服务器
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Enabled {
		klog.Info("Dashboard is disabled, skipping start")
		return nil
	}

	klog.Infof("Starting dashboard server on port %d", s.config.Port)

	// 启动数据聚合器
	go s.aggregator.Start(ctx)

	// 启动查看器清理任务
	if s.config.Viewer.Enabled {
		go s.viewer.Start(ctx)
	}

	// 设置HTTP路由
	mux := s.setupRoutes()

	// 创建HTTP服务器
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.Port),
		Handler: mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 启动服务器的goroutine
	errChan := make(chan error, 1)
	go func() {
		klog.Infof("Dashboard server listening on http://0.0.0.0:%d", s.config.Port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("dashboard server failed: %w", err)
		}
	}()

	// 等待上下文取消或服务器错误
	select {
	case <-ctx.Done():
		klog.Info("Shutting down dashboard server...")
		
		// 优雅关闭
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("Dashboard server shutdown error: %v", err)
		}
		
		return nil
	case err := <-errChan:
		return err
	}
}

// setupRoutes 设置HTTP路由
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// HTMX页面路由
	mux.HandleFunc("/dashboard", s.handlers.HandleDashboard)
	mux.HandleFunc("/instances", s.handlers.HandleInstancesPage)
	mux.HandleFunc("/coredumps", s.handlers.HandleCoredumpsPage)

	// API路由（JSON）
	mux.HandleFunc("/api/v1/health", s.handlers.corsMiddleware(s.handlers.HandleHealth))
	mux.HandleFunc("/api/v1/summary", s.handlers.corsMiddleware(s.handlers.HandleSummary))
	mux.HandleFunc("/api/v1/instances", s.handlers.corsMiddleware(s.handlers.HandleInstances))
	mux.HandleFunc("/api/v1/coredumps", s.handlers.corsMiddleware(s.handlers.HandleCoredumps))
	mux.HandleFunc("/api/v1/metrics", s.handlers.corsMiddleware(s.handlers.HandleMetrics))

	// 动态路由处理
	mux.HandleFunc("/api/v1/", s.handlers.corsMiddleware(s.handleDynamicAPI))

	// 静态文件服务
	if s.config.ServeStatic {
		staticFS := http.FileServer(http.Dir(s.config.StaticPath))
		mux.Handle("/static/", http.StripPrefix("/static/", staticFS))
		
		// 根路径重定向到主页
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.ServeFile(w, r, s.config.StaticPath+"/index.html")
			} else {
				http.NotFound(w, r)
			}
		})
	}

	return mux
}

// handleDynamicAPI 处理动态API路由
func (s *Server) handleDynamicAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	switch {
	// 实例Pod详情: /api/v1/instances/{name}/pods
	case matchRoute(path, "/api/v1/instances/", "/pods"):
		s.handlers.HandleInstancePods(w, r)
		
	// Coredump详情: /api/v1/coredumps/{id}
	case matchRoute(path, "/api/v1/coredumps/", "") && r.Method == "GET":
		s.handlers.HandleCoredumpDetail(w, r)
		
	// 创建查看器: /api/v1/coredumps/{id}/view
	case matchRoute(path, "/api/v1/coredumps/", "/view") && r.Method == "POST":
		s.handlers.HandleCreateViewer(w, r)
		
	// 查看器状态: /api/v1/viewers/{id}
	case matchRoute(path, "/api/v1/viewers/", "") && r.Method == "GET":
		s.handlers.HandleViewerStatus(w, r)
		
	// 停止查看器: /api/v1/viewers/{id}
	case matchRoute(path, "/api/v1/viewers/", "") && r.Method == "DELETE":
		s.handlers.HandleStopViewer(w, r)
		
	default:
		http.NotFound(w, r)
	}
}

// matchRoute 检查路径是否匹配特定的路由模式
func matchRoute(path, prefix, suffix string) bool {
	if !startsWith(path, prefix) {
		return false
	}
	
	if suffix == "" {
		// 确保路径中prefix后面有内容，但不包含额外的路径段
		remaining := path[len(prefix):]
		return remaining != "" && !contains(remaining, "/")
	}
	
	return endsWith(path, suffix)
}

// 辅助函数
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// GetMetrics 返回Dashboard自身的指标
func (s *Server) GetMetrics() map[string]interface{} {
	activeViewers := len(s.viewer.GetActiveViewers())
	
	return map[string]interface{}{
		"dashboard_enabled":      s.config.Enabled,
		"dashboard_port":         s.config.Port,
		"viewer_enabled":         s.config.Viewer.Enabled,
		"active_viewers":         activeViewers,
		"max_concurrent_viewers": s.config.Viewer.MaxConcurrentPods,
	}
}