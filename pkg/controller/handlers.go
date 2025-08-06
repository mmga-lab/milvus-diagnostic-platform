package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// API request/response types
type AIAnalysisRequest struct {
	NodeName       string  `json:"nodeName"`
	CoredumpPath   string  `json:"coredumpPath"`
	ValueScore     float64 `json:"valueScore"`
	EstimatedCost  float64 `json:"estimatedCost"`
	Priority       string  `json:"priority"` // high, medium, low
}

type AIAnalysisResponse struct {
	Allowed       bool    `json:"allowed"`
	Reason        string  `json:"reason,omitempty"`
	RemainingCost float64 `json:"remainingCost"`
}

type CleanupRequest struct {
	NodeName      string `json:"nodeName"`
	InstanceName  string `json:"instanceName"`
	Namespace     string `json:"namespace"`
	RestartCount  int    `json:"restartCount"`
	DeploymentType string `json:"deploymentType"` // helm, operator
}

type CleanupResponse struct {
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason,omitempty"`
	TaskID     string `json:"taskId,omitempty"`
	AssignedTo string `json:"assignedTo,omitempty"`
}

type HeartbeatRequest struct {
	NodeName string `json:"nodeName"`
	Version  string `json:"version"`
	Status   string `json:"status"`
}

type HeartbeatResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

type StatsResponse struct {
	GlobalState *GlobalState           `json:"globalState"`
	Agents      map[string]*AgentInfo  `json:"agents"`
}

// HandleAIAnalysisRequest handles requests for AI analysis permission
func (m *Manager) HandleAIAnalysisRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AIAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	klog.V(4).Infof("AI analysis request from node %s for %s (score: %.2f, cost: $%.2f)", 
		req.NodeName, req.CoredumpPath, req.ValueScore, req.EstimatedCost)

	response := m.processAIAnalysisRequest(&req)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode AI analysis response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// processAIAnalysisRequest determines whether to allow AI analysis
func (m *Manager) processAIAnalysisRequest(req *AIAnalysisRequest) *AIAnalysisResponse {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	config := m.config.Analyzer.AIAnalysis
	
	// Check if AI analysis is globally enabled
	if !config.Enabled {
		return &AIAnalysisResponse{
			Allowed: false,
			Reason:  "AI analysis is disabled",
		}
	}

	// Check monthly cost limit
	if m.globalState.MonthlyAICost+req.EstimatedCost > config.MaxCostPerMonth {
		remainingCost := config.MaxCostPerMonth - m.globalState.MonthlyAICost
		return &AIAnalysisResponse{
			Allowed:       false,
			Reason:        fmt.Sprintf("Monthly cost limit would be exceeded (remaining: $%.2f)", remainingCost),
			RemainingCost: remainingCost,
		}
	}

	// Check hourly analysis limit
	if m.globalState.AIAnalysisCount >= config.MaxAnalysisPerHour {
		return &AIAnalysisResponse{
			Allowed: false,
			Reason:  "Hourly analysis limit exceeded",
		}
	}

	// Approve the request and update state
	m.globalState.MonthlyAICost += req.EstimatedCost
	m.globalState.AIAnalysisCount++
	m.globalState.LastUpdated = time.Now()

	klog.Infof("Approved AI analysis for %s (cost: $%.2f, monthly total: $%.2f)", 
		req.CoredumpPath, req.EstimatedCost, m.globalState.MonthlyAICost)

	return &AIAnalysisResponse{
		Allowed:       true,
		RemainingCost: config.MaxCostPerMonth - m.globalState.MonthlyAICost,
	}
}

// HandleCleanupRequest handles requests for instance cleanup permission
func (m *Manager) HandleCleanupRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	klog.V(4).Infof("Cleanup request from node %s for %s/%s (restarts: %d)", 
		req.NodeName, req.Namespace, req.InstanceName, req.RestartCount)

	response := m.processCleanupRequest(&req)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode cleanup response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// processCleanupRequest determines whether to allow instance cleanup
func (m *Manager) processCleanupRequest(req *CleanupRequest) *CleanupResponse {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	instanceKey := fmt.Sprintf("%s/%s", req.Namespace, req.InstanceName)

	// Check if cleanup is globally enabled
	if !m.config.Cleaner.Enabled {
		return &CleanupResponse{
			Allowed: false,
			Reason:  "Instance cleanup is disabled",
		}
	}

	// Check if restart count meets threshold
	if req.RestartCount < m.config.Cleaner.MaxRestartCount {
		return &CleanupResponse{
			Allowed: false,
			Reason:  fmt.Sprintf("Restart count (%d) below threshold (%d)", req.RestartCount, m.config.Cleaner.MaxRestartCount),
		}
	}

	// Check if already scheduled for cleanup
	if existingTask, exists := m.globalState.PendingCleanups[instanceKey]; exists {
		if existingTask.Status == "pending" || existingTask.Status == "in_progress" {
			return &CleanupResponse{
				Allowed:    false,
				Reason:     "Cleanup already scheduled or in progress",
				TaskID:     instanceKey,
				AssignedTo: existingTask.AssignedAgent,
			}
		}
	}

	// Create new cleanup task
	taskID := instanceKey
	task := &CleanupTask{
		InstanceName:  req.InstanceName,
		Namespace:     req.Namespace,
		RestartCount:  req.RestartCount,
		ScheduledAt:   time.Now(),
		AssignedAgent: req.NodeName,
		Status:        "pending",
	}

	m.globalState.PendingCleanups[taskID] = task

	klog.Infof("Approved cleanup for %s (assigned to %s)", instanceKey, req.NodeName)

	return &CleanupResponse{
		Allowed:    true,
		TaskID:     taskID,
		AssignedTo: req.NodeName,
	}
}

// HandleHeartbeat handles agent heartbeat requests
func (m *Manager) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	m.agentsMutex.Lock()
	agent, exists := m.agents[req.NodeName]
	if !exists {
		agent = &AgentInfo{
			NodeName: req.NodeName,
		}
		m.agents[req.NodeName] = agent
		klog.Infof("New agent registered: %s", req.NodeName)
	}
	
	agent.LastHeartbeat = time.Now()
	agent.Version = req.Version
	agent.Status = "active"
	m.agentsMutex.Unlock()

	response := &HeartbeatResponse{
		Acknowledged: true,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode heartbeat response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// HandleGetStats handles requests for global statistics
func (m *Manager) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.stateMutex.RLock()
	globalStateCopy := *m.globalState
	m.stateMutex.RUnlock()

	m.agentsMutex.RLock()
	agentsCopy := make(map[string]*AgentInfo)
	for k, v := range m.agents {
		agentsCopy[k] = &AgentInfo{
			NodeName:      v.NodeName,
			LastHeartbeat: v.LastHeartbeat,
			Version:       v.Version,
			Status:        v.Status,
		}
	}
	m.agentsMutex.RUnlock()

	response := &StatsResponse{
		GlobalState: &globalStateCopy,
		Agents:      agentsCopy,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode stats response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}