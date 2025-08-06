package logcollector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"
	"milvus-diagnostic-platform/pkg/config"
	"milvus-diagnostic-platform/pkg/discovery"
)

// LogCollector handles log collection from Loki
type LogCollector struct {
	config     *config.LogCollectorConfig
	parser     *LogParser
	discovery  *discovery.Discovery
	httpClient *http.Client
	
	// Event channel
	eventChan chan LogCollectionEvent
	
	// Last query timestamps
	lastQueryTime map[string]time.Time
	queryMux      sync.RWMutex
	
	// Processing state
	running bool
	stopCh  chan struct{}
}

// LokiQueryRangeResponse represents Loki API query_range response
type LokiQueryRangeResponse struct {
	Status string `json:"status"`
	Data   LokiQueryRangeData `json:"data"`
}

type LokiQueryRangeData struct {
	ResultType string      `json:"resultType"`
	Result     []LokiStream `json:"result"`
}

type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// LokiLogEntry represents a single log entry from Loki
type LokiLogEntry struct {
	Timestamp time.Time
	Line      string
	Stream    map[string]string
}

// NewLogCollector creates a new log collector
func NewLogCollector(config *config.LogCollectorConfig, discovery *discovery.Discovery) *LogCollector {
	return &LogCollector{
		config:        config,
		parser:        NewLogParser(config),
		discovery:     discovery,
		httpClient:    &http.Client{Timeout: config.Loki.Timeout},
		eventChan:     make(chan LogCollectionEvent, config.BufferSize),
		lastQueryTime: make(map[string]time.Time),
		stopCh:        make(chan struct{}),
	}
}

// Start begins log collection from Loki
func (lc *LogCollector) Start(ctx context.Context) error {
	if !lc.config.Enabled {
		klog.Info("Log collector is disabled")
		return nil
	}
	
	if lc.config.Source != "loki" {
		return fmt.Errorf("unsupported log source: %s", lc.config.Source)
	}
	
	klog.Info("Starting log collector with Loki source")
	lc.running = true
	
	// Initialize last query times
	lc.initializeQueryTimes()
	
	// Start query goroutines for each configured query
	for _, query := range lc.config.Loki.Queries {
		go lc.queryLokiPeriodically(ctx, query)
	}
	
	// Wait for context cancellation
	<-ctx.Done()
	lc.stop()
	return nil
}

// GetEventChannel returns the event channel
func (lc *LogCollector) GetEventChannel() <-chan LogCollectionEvent {
	return lc.eventChan
}

// stop stops the log collector
func (lc *LogCollector) stop() {
	if !lc.running {
		return
	}
	
	klog.Info("Stopping log collector")
	lc.running = false
	close(lc.stopCh)
	close(lc.eventChan)
}

// initializeQueryTimes sets up initial query timestamps
func (lc *LogCollector) initializeQueryTimes() {
	lc.queryMux.Lock()
	defer lc.queryMux.Unlock()
	
	now := time.Now()
	for _, query := range lc.config.Loki.Queries {
		// Start querying from lookback window ago
		lc.lastQueryTime[query.Name] = now.Add(-lc.config.Loki.LookbackWindow)
	}
}

// queryLokiPeriodically runs a Loki query periodically
func (lc *LogCollector) queryLokiPeriodically(ctx context.Context, query config.LokiQuery) {
	ticker := time.NewTicker(lc.config.Loki.QueryInterval)
	defer ticker.Stop()
	
	klog.V(4).Infof("Starting periodic Loki query: %s", query.Name)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-lc.stopCh:
			return
		case <-ticker.C:
			if err := lc.executeLokiQuery(query); err != nil {
				klog.Errorf("Failed to execute Loki query %s: %v", query.Name, err)
				lc.sendEvent(LogCollectionEvent{
					Type:      EventTypeLogError,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			}
		}
	}
}

// executeLokiQuery executes a single Loki query
func (lc *LogCollector) executeLokiQuery(query config.LokiQuery) error {
	lc.queryMux.RLock()
	lastTime := lc.lastQueryTime[query.Name]
	lc.queryMux.RUnlock()
	
	now := time.Now()
	
	// Build query URL
	lokiURL, err := lc.buildLokiQueryURL(query.Query, lastTime, now)
	if err != nil {
		return fmt.Errorf("failed to build Loki URL: %w", err)
	}
	
	// Execute HTTP request
	resp, err := lc.httpClient.Get(lokiURL)
	if err != nil {
		return fmt.Errorf("failed to query Loki: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Loki query failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse response
	var lokiResp LokiQueryRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return fmt.Errorf("failed to parse Loki response: %w", err)
	}
	
	// Process log entries
	logCount := 0
	for _, stream := range lokiResp.Data.Result {
		for _, value := range stream.Values {
			if len(value) >= 2 {
				entry, err := lc.parseLokiEntry(value, stream.Stream, query)
				if err != nil {
					klog.V(4).Infof("Failed to parse log entry: %v", err)
					continue
				}
				
				lc.sendEvent(LogCollectionEvent{
					Type:      EventTypeLogDiscovered,
					LogEntry:  entry,
					Timestamp: time.Now(),
				})
				logCount++
			}
		}
	}
	
	// Update last query time
	lc.queryMux.Lock()
	lc.lastQueryTime[query.Name] = now
	lc.queryMux.Unlock()
	
	if logCount > 0 {
		klog.V(4).Infof("Processed %d log entries from query: %s", logCount, query.Name)
	}
	
	return nil
}

// buildLokiQueryURL builds the Loki query_range URL
func (lc *LogCollector) buildLokiQueryURL(query string, start, end time.Time) (string, error) {
	baseURL := strings.TrimRight(lc.config.Loki.URL, "/")
	queryURL := fmt.Sprintf("%s/loki/api/v1/query_range", baseURL)
	
	// Build query parameters
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	params.Set("limit", strconv.Itoa(lc.config.Loki.BatchSize))
	params.Set("direction", "forward")
	
	return queryURL + "?" + params.Encode(), nil
}

// parseLokiEntry parses a single log entry from Loki response
func (lc *LogCollector) parseLokiEntry(value []string, stream map[string]string, query config.LokiQuery) (*LogEntry, error) {
	if len(value) < 2 {
		return nil, fmt.Errorf("invalid log entry format")
	}
	
	// Parse timestamp (nanoseconds since Unix epoch)
	timestampNs, err := strconv.ParseInt(value[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}
	timestamp := time.Unix(0, timestampNs)
	
	// Get log line
	line := value[1]
	
	// Parse using existing parser
	entry, err := lc.parser.ParseLine(line, query.Name, 0)
	if err != nil {
		return nil, err
	}
	
	// Override timestamp with Loki timestamp
	entry.Timestamp = timestamp
	
	// Extract information from Loki stream labels
	if namespace, ok := stream["namespace"]; ok {
		entry.Namespace = namespace
	}
	if pod, ok := stream["pod"]; ok {
		entry.PodName = pod
	}
	if container, ok := stream["container"]; ok {
		entry.ContainerName = container
	}
	if job, ok := stream["job"]; ok {
		entry.Component = job
	}
	
	// Enrich with instance information
	lc.enrichWithInstanceInfo(entry)
	
	return entry, nil
}

// enrichWithInstanceInfo enriches log entry with Milvus instance information
func (lc *LogCollector) enrichWithInstanceInfo(entry *LogEntry) {
	if lc.discovery == nil {
		return
	}
	
	// Try to match with discovered instances based on namespace and pod name
	instances := lc.discovery.GetInstances()
	for _, instance := range instances {
		if entry.Namespace != "" && instance.Namespace == entry.Namespace {
			// Check if pod belongs to this instance
			for _, pod := range instance.Pods {
				if entry.PodName != "" && pod.Name == entry.PodName {
					entry.InstanceName = instance.Name
					return
				}
			}
		}
	}
}

// sendEvent sends an event to the event channel
func (lc *LogCollector) sendEvent(event LogCollectionEvent) {
	select {
	case lc.eventChan <- event:
	default:
		klog.Warning("Event channel full, dropping log collection event")
	}
}