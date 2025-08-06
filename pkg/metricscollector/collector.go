package metricscollector

import (
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"milvus-diagnostic-platform/pkg/config"
	"milvus-diagnostic-platform/pkg/discovery"
)

// MetricsCollector handles metric collection from Prometheus
type MetricsCollector struct {
	config     *config.MetricsCollectorConfig
	discovery  *discovery.Discovery
	httpClient *http.Client
	
	// Event channel
	eventChan chan MetricCollectionEvent
	
	// Last query timestamps
	lastQueryTime map[string]time.Time
	queryMux      sync.RWMutex
	
	// Processing state
	running bool
	stopCh  chan struct{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(config *config.MetricsCollectorConfig, discovery *discovery.Discovery) *MetricsCollector {
	return &MetricsCollector{
		config:        config,
		discovery:     discovery,
		httpClient:    &http.Client{Timeout: config.Prometheus.Timeout},
		eventChan:     make(chan MetricCollectionEvent, 1000), // Default buffer size
		lastQueryTime: make(map[string]time.Time),
		stopCh:        make(chan struct{}),
	}
}

// Start begins metric collection from Prometheus
func (mc *MetricsCollector) Start(ctx context.Context) error {
	if !mc.config.Enabled {
		klog.Info("Metrics collector is disabled")
		return nil
	}
	
	if mc.config.Source != "prometheus" {
		return fmt.Errorf("unsupported metrics source: %s", mc.config.Source)
	}
	
	klog.Info("Starting metrics collector with Prometheus source")
	mc.running = true
	
	// Initialize last query times
	mc.initializeQueryTimes()
	
	// Start query goroutines for each configured query
	for _, query := range mc.config.Prometheus.Queries {
		go mc.queryPrometheusPeriodically(ctx, query)
	}
	
	// Wait for context cancellation
	<-ctx.Done()
	mc.stop()
	return nil
}

// GetEventChannel returns the event channel
func (mc *MetricsCollector) GetEventChannel() <-chan MetricCollectionEvent {
	return mc.eventChan
}

// stop stops the metrics collector
func (mc *MetricsCollector) stop() {
	if !mc.running {
		return
	}
	
	klog.Info("Stopping metrics collector")
	mc.running = false
	close(mc.stopCh)
	close(mc.eventChan)
}

// initializeQueryTimes sets up initial query timestamps
func (mc *MetricsCollector) initializeQueryTimes() {
	mc.queryMux.Lock()
	defer mc.queryMux.Unlock()
	
	now := time.Now()
	for _, query := range mc.config.Prometheus.Queries {
		// Start querying from lookback window ago
		mc.lastQueryTime[query.Name] = now.Add(-mc.config.Prometheus.LookbackWindow)
	}
}

// queryPrometheusPeriodically runs a Prometheus query periodically
func (mc *MetricsCollector) queryPrometheusPeriodically(ctx context.Context, query config.PrometheusQuery) {
	ticker := time.NewTicker(mc.config.Prometheus.QueryInterval)
	defer ticker.Stop()
	
	klog.V(4).Infof("Starting periodic Prometheus query: %s", query.Name)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-mc.stopCh:
			return
		case <-ticker.C:
			if err := mc.executePrometheusQuery(query); err != nil {
				klog.Errorf("Failed to execute Prometheus query %s: %v", query.Name, err)
				mc.sendEvent(MetricCollectionEvent{
					Type:      EventTypeMetricError,
					Error:     err.Error(),
					Timestamp: time.Now(),
					QueryName: query.Name,
				})
			}
		}
	}
}

// executePrometheusQuery executes a single Prometheus query
func (mc *MetricsCollector) executePrometheusQuery(query config.PrometheusQuery) error {
	mc.queryMux.RLock()
	lastTime := mc.lastQueryTime[query.Name]
	mc.queryMux.RUnlock()
	
	now := time.Now()
	
	// Build query URL - use query_range for time series data
	prometheusURL, err := mc.buildPrometheusQueryURL(query.Query, lastTime, now)
	if err != nil {
		return fmt.Errorf("failed to build Prometheus URL: %w", err)
	}
	
	// Execute HTTP request
	resp, err := mc.httpClient.Get(prometheusURL)
	if err != nil {
		return fmt.Errorf("failed to query Prometheus: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Prometheus query failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse response
	var promResp PrometheusQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
		return fmt.Errorf("failed to parse Prometheus response: %w", err)
	}
	
	// Process metric entries
	metricCount := 0
	for _, result := range promResp.Data.Result {
		// Handle both instant and range query results
		if len(result.Values) > 0 {
			// Range query result
			for _, value := range result.Values {
				entry, err := mc.parsePrometheusValue(value, result.Metric, query)
				if err != nil {
					klog.V(4).Infof("Failed to parse metric value: %v", err)
					continue
				}
				
				mc.sendEvent(MetricCollectionEvent{
					Type:        EventTypeMetricDiscovered,
					MetricEntry: entry,
					Timestamp:   time.Now(),
					QueryName:   query.Name,
				})
				metricCount++
			}
		} else if len(result.Value) > 0 {
			// Instant query result
			entry, err := mc.parsePrometheusValue(result.Value, result.Metric, query)
			if err != nil {
				klog.V(4).Infof("Failed to parse metric value: %v", err)
				continue
			}
			
			mc.sendEvent(MetricCollectionEvent{
				Type:        EventTypeMetricDiscovered,
				MetricEntry: entry,
				Timestamp:   time.Now(),
				QueryName:   query.Name,
			})
			metricCount++
		}
	}
	
	// Update last query time
	mc.queryMux.Lock()
	mc.lastQueryTime[query.Name] = now
	mc.queryMux.Unlock()
	
	if metricCount > 0 {
		klog.V(4).Infof("Processed %d metric entries from query: %s", metricCount, query.Name)
	}
	
	return nil
}

// buildPrometheusQueryURL builds the Prometheus query_range URL
func (mc *MetricsCollector) buildPrometheusQueryURL(query string, start, end time.Time) (string, error) {
	baseURL := strings.TrimRight(mc.config.Prometheus.URL, "/")
	queryURL := fmt.Sprintf("%s/api/v1/query_range", baseURL)
	
	// Build query parameters
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", "30s") // Default step, could be configurable
	
	return queryURL + "?" + params.Encode(), nil
}

// parsePrometheusValue parses a single metric value from Prometheus response
func (mc *MetricsCollector) parsePrometheusValue(value []interface{}, metric map[string]string, query config.PrometheusQuery) (*MetricEntry, error) {
	if len(value) < 2 {
		return nil, fmt.Errorf("invalid metric value format")
	}
	
	// Parse timestamp
	var timestamp time.Time
	switch ts := value[0].(type) {
	case float64:
		timestamp = time.Unix(int64(ts), 0)
	default:
		return nil, fmt.Errorf("invalid timestamp format")
	}
	
	// Parse value
	var metricValue float64
	switch val := value[1].(type) {
	case string:
		var err error
		metricValue, err = strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse metric value: %w", err)
		}
	case float64:
		metricValue = val
	default:
		return nil, fmt.Errorf("invalid value format")
	}
	
	// Create metric entry
	entry := &MetricEntry{
		MetricName: query.Name,
		MetricType: mc.determineMetricType(query.Name),
		Value:      metricValue,
		Labels:     metric,
		Timestamp:  timestamp,
		Source:     mc.determineMetricSource(metric),
		CreatedAt:  metav1.Now(),
	}
	
	// Extract standard labels
	if namespace, ok := metric["namespace"]; ok {
		entry.Namespace = namespace
	}
	if pod, ok := metric["pod"]; ok {
		entry.PodName = pod
	}
	if container, ok := metric["container"]; ok {
		entry.ContainerName = container
	}
	if instance, ok := metric["instance"]; ok {
		entry.NodeName = instance
	}
	
	// Enrich with instance information
	mc.enrichWithInstanceInfo(entry)
	
	return entry, nil
}

// determineMetricType determines the metric type based on the metric name
func (mc *MetricsCollector) determineMetricType(metricName string) string {
	// Simple heuristics - could be enhanced
	if strings.Contains(metricName, "_total") || strings.Contains(metricName, "_count") {
		return "counter"
	}
	if strings.Contains(metricName, "_bucket") || strings.Contains(metricName, "_histogram") {
		return "histogram"
	}
	return "gauge"
}

// determineMetricSource determines the metric source based on labels
func (mc *MetricsCollector) determineMetricSource(labels map[string]string) string {
	if job, ok := labels["job"]; ok {
		if strings.Contains(job, "node") {
			return "system"
		}
		if strings.Contains(job, "kube") {
			return "kubernetes"
		}
		if strings.Contains(job, "milvus") {
			return "application"
		}
	}
	return "custom"
}

// enrichWithInstanceInfo enriches metric entry with Milvus instance information
func (mc *MetricsCollector) enrichWithInstanceInfo(entry *MetricEntry) {
	if mc.discovery == nil {
		return
	}
	
	// Try to match with discovered instances based on namespace and pod name
	instances := mc.discovery.GetInstances()
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
func (mc *MetricsCollector) sendEvent(event MetricCollectionEvent) {
	select {
	case mc.eventChan <- event:
	default:
		klog.Warning("Event channel full, dropping metric collection event")
	}
}