package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// Client provides interface for agents to communicate with controller
type Client struct {
	baseURL    string
	httpClient *http.Client
	nodeName   string
	version    string
	
	// Heartbeat management
	heartbeatInterval time.Duration
	heartbeatCtx      context.Context
	heartbeatCancel   context.CancelFunc
}

// ClientConfig holds configuration for controller client
type ClientConfig struct {
	ControllerURL     string        `json:"controllerUrl"`
	Timeout           time.Duration `json:"timeout"`
	HeartbeatInterval time.Duration `json:"heartbeatInterval"`
	NodeName          string        `json:"nodeName"`
	Version           string        `json:"version"`
}

// NewClient creates a new controller client
func NewClient(config *ClientConfig) *Client {
	return &Client{
		baseURL: config.ControllerURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		nodeName:          config.NodeName,
		version:           config.Version,
		heartbeatInterval: config.HeartbeatInterval,
	}
}

// Start begins the client (starts heartbeat)
func (c *Client) Start(ctx context.Context) error {
	c.heartbeatCtx, c.heartbeatCancel = context.WithCancel(ctx)
	
	// Start heartbeat loop
	go c.heartbeatLoop()
	
	klog.Infof("Controller client started (controller: %s, node: %s)", c.baseURL, c.nodeName)
	return nil
}

// Stop stops the client
func (c *Client) Stop() {
	if c.heartbeatCancel != nil {
		c.heartbeatCancel()
	}
}

// RequestAIAnalysis requests permission to perform AI analysis
func (c *Client) RequestAIAnalysis(coredumpPath string, valueScore float64, estimatedCost float64, priority string) (bool, string, error) {
	request := &AIAnalysisRequest{
		NodeName:      c.nodeName,
		CoredumpPath:  coredumpPath,
		ValueScore:    valueScore,
		EstimatedCost: estimatedCost,
		Priority:      priority,
	}

	var response AIAnalysisResponse
	if err := c.makeRequest("POST", "/api/ai-analysis/request", request, &response); err != nil {
		return false, "", fmt.Errorf("failed to request AI analysis: %w", err)
	}

	klog.V(4).Infof("AI analysis request response: allowed=%v, reason=%s", response.Allowed, response.Reason)
	return response.Allowed, response.Reason, nil
}

// RequestCleanup requests permission to cleanup an instance
func (c *Client) RequestCleanup(instanceName, namespace string, restartCount int, deploymentType string) (bool, string, string, error) {
	request := &CleanupRequest{
		NodeName:       c.nodeName,
		InstanceName:   instanceName,
		Namespace:      namespace,
		RestartCount:   restartCount,
		DeploymentType: deploymentType,
	}

	var response CleanupResponse
	if err := c.makeRequest("POST", "/api/cleanup/request", request, &response); err != nil {
		return false, "", "", fmt.Errorf("failed to request cleanup: %w", err)
	}

	klog.V(4).Infof("Cleanup request response: allowed=%v, reason=%s, taskID=%s", 
		response.Allowed, response.Reason, response.TaskID)
	return response.Allowed, response.Reason, response.TaskID, nil
}

// GetStats retrieves global statistics from controller
func (c *Client) GetStats() (*StatsResponse, error) {
	var response StatsResponse
	if err := c.makeRequest("GET", "/api/stats", nil, &response); err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return &response, nil
}

// sendHeartbeat sends a heartbeat to the controller
func (c *Client) sendHeartbeat() error {
	request := &HeartbeatRequest{
		NodeName: c.nodeName,
		Version:  c.version,
		Status:   "active",
	}

	var response HeartbeatResponse
	if err := c.makeRequest("POST", "/api/heartbeat", request, &response); err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	klog.V(5).Infof("Heartbeat acknowledged by controller")
	return nil
}

// heartbeatLoop sends periodic heartbeats to the controller
func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat
	if err := c.sendHeartbeat(); err != nil {
		klog.Errorf("Failed to send initial heartbeat: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				klog.Errorf("Failed to send heartbeat: %v", err)
			}
		case <-c.heartbeatCtx.Done():
			klog.Info("Heartbeat loop stopped")
			return
		}
	}
}

// makeRequest makes HTTP request to controller
func (c *Client) makeRequest(method, endpoint string, requestBody interface{}, responseBody interface{}) error {
	var reqBody *bytes.Buffer
	
	if requestBody != nil {
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	if responseBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// IsControllerAvailable checks if the controller is reachable
func (c *Client) IsControllerAvailable() bool {
	req, err := http.NewRequest("GET", c.baseURL+"/healthz", nil)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}