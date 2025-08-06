package logcollector

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LogEntry represents a parsed log entry
type LogEntry struct {
	ID           int       `json:"id"`
	SourceFile   string    `json:"sourceFile"`
	LineNumber   int       `json:"lineNumber"`
	Timestamp    time.Time `json:"timestamp"`
	Level        string    `json:"level"`
	Component    string    `json:"component"`
	Namespace    string    `json:"namespace"`
	Message      string    `json:"message"`
	RawLine      string    `json:"rawLine"`
	
	// Pattern matching results
	IsError      bool      `json:"isError"`
	IsWarning    bool      `json:"isWarning"`
	ErrorPattern string    `json:"errorPattern"`
	
	// Associated instance information
	InstanceName  string   `json:"instanceName"`
	PodName       string   `json:"podName"`
	ContainerName string   `json:"containerName"`
	
	// Analysis results
	IsAnalyzed    bool     `json:"isAnalyzed"`
	SeverityScore float64  `json:"severityScore"`
	AnalysisTime  time.Time `json:"analysisTime,omitempty"`
	
	CreatedAt metav1.Time `json:"createdAt"`
}

// LogCollectionEvent represents a log collection event
type LogCollectionEvent struct {
	Type      EventType  `json:"type"`
	LogEntry  *LogEntry  `json:"logEntry,omitempty"`
	Error     string     `json:"error,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
	SourceFile string    `json:"sourceFile,omitempty"`
}

// EventType represents different types of log collection events
type EventType string

const (
	EventTypeLogDiscovered EventType = "log_discovered"
	EventTypeLogProcessed  EventType = "log_processed"
	EventTypeLogError      EventType = "log_error"
	EventTypeLogSkipped    EventType = "log_skipped"
	EventTypeFileRotated   EventType = "file_rotated"
)

// LogLevel represents log severity levels
type LogLevel string

const (
	LogLevelError   LogLevel = "ERROR"
	LogLevelWarning LogLevel = "WARN"
	LogLevelInfo    LogLevel = "INFO"
	LogLevelDebug   LogLevel = "DEBUG"
	LogLevelUnknown LogLevel = "UNKNOWN"
)

// FileWatcher represents a watched log file
type FileWatcher struct {
	Path         string    `json:"path"`
	LastModTime  time.Time `json:"lastModTime"`
	LastPosition int64     `json:"lastPosition"`
	IsActive     bool      `json:"isActive"`
}

// ParsedLog represents the result of parsing a log line
type ParsedLog struct {
	Entry    *LogEntry
	Patterns []string // matched patterns
}

// LogAnalysisResult represents the analysis result for a log entry
type LogAnalysisResult struct {
	EntryID       int            `json:"entryId"`
	SeverityScore float64        `json:"severityScore"`
	MatchedRules  []string       `json:"matchedRules"`
	Correlations  []Correlation  `json:"correlations"`
	AIAnalysis    *AILogAnalysis `json:"aiAnalysis,omitempty"`
}

// Correlation represents a correlation between log entries and other events
type Correlation struct {
	Type          string    `json:"type"` // "restart", "coredump", "metric_spike"
	EventID       int       `json:"eventId"`
	CorrelationID string    `json:"correlationId"`
	Timestamp     time.Time `json:"timestamp"`
	Confidence    float64   `json:"confidence"`
}

// AILogAnalysis represents AI analysis results for log entries
type AILogAnalysis struct {
	Summary        string   `json:"summary"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Recommendations []string `json:"recommendations"`
	Confidence     float64  `json:"confidence"`
	TokensUsed     int      `json:"tokensUsed"`
	CostUSD        float64  `json:"costUsd"`
}