package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"milvus-diagnostic-platform/pkg/config"
)

// DatabaseInterface defines the interface for database operations
type DatabaseInterface interface {
	Close() error
	// Add other database methods as needed
}

// Manager coordinates global state and decisions across all agents
type Manager struct {
	config     *config.Config
	kubeClient kubernetes.Interface
	database   DatabaseInterface
	
	// Global state
	globalState *GlobalState
	stateMutex  sync.RWMutex
	
	// Agent tracking
	agents map[string]*AgentInfo
	agentsMutex sync.RWMutex
}

type Config struct {
	Config     *config.Config
	KubeClient kubernetes.Interface
	Database   DatabaseInterface
}

// GlobalState tracks cluster-wide state
type GlobalState struct {
	// AI Analysis tracking
	MonthlyAICost       float64                 `json:"monthlyAiCost"`
	AIAnalysisCount     int                     `json:"aiAnalysisCount"`
	LastAIAnalysisReset time.Time               `json:"lastAiAnalysisReset"`
	
	// Instance cleanup tracking
	PendingCleanups     map[string]*CleanupTask `json:"pendingCleanups"`
	CompletedCleanups   []*CleanupRecord        `json:"completedCleanups"`
	
	// Global statistics
	TotalCoredumps      int `json:"totalCoredumps"`
	HighValueCoredumps  int `json:"highValueCoredumps"`
	TotalInstances      int `json:"totalInstances"`
	
	LastUpdated         time.Time `json:"lastUpdated"`
}

// CleanupTask represents a pending cleanup operation
type CleanupTask struct {
	InstanceName  string    `json:"instanceName"`
	Namespace     string    `json:"namespace"`
	RestartCount  int       `json:"restartCount"`
	ScheduledAt   time.Time `json:"scheduledAt"`
	AssignedAgent string    `json:"assignedAgent"`
	Status        string    `json:"status"` // pending, in_progress, completed, failed
}

// CleanupRecord represents a completed cleanup operation
type CleanupRecord struct {
	InstanceName string    `json:"instanceName"`
	Namespace    string    `json:"namespace"`
	CompletedAt  time.Time `json:"completedAt"`
	Success      bool      `json:"success"`
	Agent        string    `json:"agent"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

// AgentInfo tracks individual agent status
type AgentInfo struct {
	NodeName      string    `json:"nodeName"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	Version       string    `json:"version"`
	Status        string    `json:"status"` // active, inactive, error
}

// NewManager creates a new controller manager
func NewManager(cfg *Config) *Manager {
	return &Manager{
		config:      cfg.Config,
		kubeClient:  cfg.KubeClient,
		database:    cfg.Database,
		globalState: &GlobalState{
			PendingCleanups:     make(map[string]*CleanupTask),
			CompletedCleanups:   make([]*CleanupRecord, 0),
			LastAIAnalysisReset: time.Now(),
			LastUpdated:         time.Now(),
		},
		agents: make(map[string]*AgentInfo),
	}
}

// Start begins the controller manager
func (m *Manager) Start(ctx context.Context) error {
	klog.Info("Starting controller manager")
	
	// Load existing state from database
	if err := m.loadState(); err != nil {
		klog.Errorf("Failed to load state from database: %v", err)
		// Continue with default state
	}
	
	// Start background tasks
	go m.stateCleanupLoop(ctx)
	go m.agentHealthCheckLoop(ctx)
	go m.statisticsUpdateLoop(ctx)
	
	klog.Info("Controller manager started successfully")
	
	<-ctx.Done()
	klog.Info("Controller manager shutting down")
	
	// Save state before shutdown
	if err := m.saveState(); err != nil {
		klog.Errorf("Failed to save state to database: %v", err)
	}
	
	return nil
}

// loadState loads global state from database
func (m *Manager) loadState() error {
	// Implementation to load state from database
	// For now, use default state
	klog.Info("Loaded global state from database")
	return nil
}

// saveState saves current global state to database
func (m *Manager) saveState() error {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	
	// Save to database
	klog.Info("Saved global state to database")
	return nil
}

// stateCleanupLoop periodically cleans up old state
func (m *Manager) stateCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.cleanupOldState()
		case <-ctx.Done():
			return
		}
	}
}

// agentHealthCheckLoop monitors agent health
func (m *Manager) agentHealthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.checkAgentHealth()
		case <-ctx.Done():
			return
		}
	}
}

// statisticsUpdateLoop updates global statistics
func (m *Manager) statisticsUpdateLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.updateGlobalStatistics()
		case <-ctx.Done():
			return
		}
	}
}

// cleanupOldState removes old records and resets counters
func (m *Manager) cleanupOldState() {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()
	
	now := time.Now()
	
	// Reset monthly AI cost if it's a new month
	if now.Month() != m.globalState.LastAIAnalysisReset.Month() || 
	   now.Year() != m.globalState.LastAIAnalysisReset.Year() {
		klog.Info("Resetting monthly AI analysis cost")
		m.globalState.MonthlyAICost = 0
		m.globalState.AIAnalysisCount = 0
		m.globalState.LastAIAnalysisReset = now
	}
	
	// Clean up old completed cleanups (keep last 100)
	if len(m.globalState.CompletedCleanups) > 100 {
		m.globalState.CompletedCleanups = m.globalState.CompletedCleanups[len(m.globalState.CompletedCleanups)-100:]
	}
	
	// Remove failed pending cleanups older than 1 hour
	for key, task := range m.globalState.PendingCleanups {
		if task.Status == "failed" && now.Sub(task.ScheduledAt) > time.Hour {
			delete(m.globalState.PendingCleanups, key)
		}
	}
}

// checkAgentHealth marks inactive agents
func (m *Manager) checkAgentHealth() {
	m.agentsMutex.Lock()
	defer m.agentsMutex.Unlock()
	
	now := time.Now()
	for nodeName, agent := range m.agents {
		if now.Sub(agent.LastHeartbeat) > 2*time.Minute {
			if agent.Status != "inactive" {
				klog.Warningf("Agent on node %s is now inactive", nodeName)
				agent.Status = "inactive"
			}
		}
	}
}

// updateGlobalStatistics updates cluster-wide statistics
func (m *Manager) updateGlobalStatistics() {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()
	
	// Query database for current statistics
	// This is a placeholder - implement actual database queries
	
	m.globalState.LastUpdated = time.Now()
	klog.V(4).Info("Updated global statistics")
}

// GetMetricsHandler returns HTTP handler for metrics
func (m *Manager) GetMetricsHandler() http.Handler {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		m.stateMutex.RLock()
		defer m.stateMutex.RUnlock()
		
		// Return Prometheus metrics
		fmt.Fprintf(w, "# HELP milvus_coredump_controller_ai_cost_monthly Monthly AI analysis cost in USD\n")
		fmt.Fprintf(w, "# TYPE milvus_coredump_controller_ai_cost_monthly gauge\n")
		fmt.Fprintf(w, "milvus_coredump_controller_ai_cost_monthly %.2f\n", m.globalState.MonthlyAICost)
		
		fmt.Fprintf(w, "# HELP milvus_coredump_controller_ai_analyses_total Total AI analyses performed this month\n")
		fmt.Fprintf(w, "# TYPE milvus_coredump_controller_ai_analyses_total counter\n")
		fmt.Fprintf(w, "milvus_coredump_controller_ai_analyses_total %d\n", m.globalState.AIAnalysisCount)
		
		fmt.Fprintf(w, "# HELP milvus_coredump_controller_pending_cleanups Pending cleanup tasks\n")
		fmt.Fprintf(w, "# TYPE milvus_coredump_controller_pending_cleanups gauge\n")
		fmt.Fprintf(w, "milvus_coredump_controller_pending_cleanups %d\n", len(m.globalState.PendingCleanups))
		
		fmt.Fprintf(w, "# HELP milvus_coredump_controller_active_agents Active agents\n")
		fmt.Fprintf(w, "# TYPE milvus_coredump_controller_active_agents gauge\n")
		
		m.agentsMutex.RLock()
		activeAgents := 0
		for _, agent := range m.agents {
			if agent.Status == "active" {
				activeAgents++
			}
		}
		m.agentsMutex.RUnlock()
		
		fmt.Fprintf(w, "milvus_coredump_controller_active_agents %d\n", activeAgents)
	})
	
	return mux
}