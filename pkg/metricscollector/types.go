package metricscollector

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricEntry represents a collected metric entry
type MetricEntry struct {
	ID          int                    `json:"id"`
	MetricName  string                 `json:"metricName"`
	MetricType  string                 `json:"metricType"` // "counter", "gauge", "histogram"
	Value       float64                `json:"value"`
	Labels      map[string]string      `json:"labels"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      string                 `json:"source"` // "system", "application", "kubernetes", "custom"
	
	// Associated instance information
	NodeName      string               `json:"nodeName,omitempty"`
	Namespace     string               `json:"namespace,omitempty"`
	PodName       string               `json:"podName,omitempty"`
	ContainerName string               `json:"containerName,omitempty"`
	InstanceName  string               `json:"instanceName,omitempty"`
	
	CreatedAt metav1.Time              `json:"createdAt"`
}

// MetricCollectionEvent represents a metric collection event
type MetricCollectionEvent struct {
	Type        EventType       `json:"type"`
	MetricEntry *MetricEntry    `json:"metricEntry,omitempty"`
	Error       string          `json:"error,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
	QueryName   string          `json:"queryName,omitempty"`
}

// EventType represents different types of metric collection events
type EventType string

const (
	EventTypeMetricDiscovered EventType = "metric_discovered"
	EventTypeMetricProcessed  EventType = "metric_processed"
	EventTypeMetricError      EventType = "metric_error"
	EventTypeQueryExecuted    EventType = "query_executed"
)

// PrometheusQueryResult represents Prometheus query result
type PrometheusQueryResult struct {
	Status string                   `json:"status"`
	Data   PrometheusQueryResultData `json:"data"`
}

type PrometheusQueryResultData struct {
	ResultType string          `json:"resultType"`
	Result     []PrometheusResult `json:"result"`
}

type PrometheusResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value,omitempty"`  // for instant queries [timestamp, value]
	Values [][]interface{}   `json:"values,omitempty"` // for range queries [[timestamp, value], ...]
}

// MetricAnalysisResult represents the analysis result for a metric
type MetricAnalysisResult struct {
	MetricName      string                `json:"metricName"`
	TrendDirection  string                `json:"trendDirection"` // "up", "down", "stable"
	AnomalyScore    float64              `json:"anomalyScore"`   // 0-10 scale
	Correlations    []MetricCorrelation  `json:"correlations"`
	Predictions     []MetricPrediction   `json:"predictions"`
	AIAnalysis      *AIMetricAnalysis    `json:"aiAnalysis,omitempty"`
}

// MetricCorrelation represents correlation between metrics
type MetricCorrelation struct {
	MetricName      string    `json:"metricName"`
	CorrelationType string    `json:"correlationType"` // "positive", "negative", "none"
	Strength        float64   `json:"strength"`        // 0-1
	Timestamp       time.Time `json:"timestamp"`
}

// MetricPrediction represents a prediction for future metric values
type MetricPrediction struct {
	Timestamp       time.Time `json:"timestamp"`
	PredictedValue  float64   `json:"predictedValue"`
	Confidence      float64   `json:"confidence"` // 0-1
	Method          string    `json:"method"`     // "linear", "exponential", "seasonal"
}

// AIMetricAnalysis represents AI analysis results for metrics
type AIMetricAnalysis struct {
	Summary         string              `json:"summary"`
	AnomalyReason   string              `json:"anomalyReason"`
	Impact          string              `json:"impact"`
	Recommendations []string            `json:"recommendations"`
	Confidence      float64             `json:"confidence"`
	TokensUsed      int                 `json:"tokensUsed"`
	CostUSD         float64             `json:"costUsd"`
}

// MetricThreshold represents thresholds for metric alerting
type MetricThreshold struct {
	MetricName    string    `json:"metricName"`
	WarningValue  float64   `json:"warningValue"`
	CriticalValue float64   `json:"criticalValue"`
	Operator      string    `json:"operator"` // ">", "<", ">=", "<=", "=="
	Duration      time.Duration `json:"duration"`
}

// MetricAlert represents an alert triggered by metric thresholds
type MetricAlert struct {
	ID          string              `json:"id"`
	MetricName  string              `json:"metricName"`
	Severity    string              `json:"severity"` // "warning", "critical"
	Value       float64             `json:"value"`
	Threshold   float64             `json:"threshold"`
	Message     string              `json:"message"`
	Labels      map[string]string   `json:"labels"`
	Timestamp   time.Time           `json:"timestamp"`
	Duration    time.Duration       `json:"duration"`
	Status      string              `json:"status"` // "firing", "resolved"
}