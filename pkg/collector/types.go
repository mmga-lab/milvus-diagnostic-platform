package collector

import (
	"time"
	
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"milvus-coredump-agent/pkg/discovery"
)

type CoredumpFile struct {
	Path        string                `json:"path"`
	FileName    string                `json:"fileName"`
	Size        int64                 `json:"size"`
	ModTime     time.Time            `json:"modTime"`
	PID         int                  `json:"pid"`
	UID         int                  `json:"uid"`
	GID         int                  `json:"gid"`
	Signal      int                  `json:"signal"`
	Timestamp   time.Time            `json:"timestamp"`
	Executable  string               `json:"executable"`
	Arguments   []string             `json:"arguments"`
	Hostname    string               `json:"hostname"`
	
	// Associated pod information
	PodName      string              `json:"podName,omitempty"`
	PodNamespace string              `json:"podNamespace,omitempty"`
	ContainerName string             `json:"containerName,omitempty"`
	InstanceName string              `json:"instanceName,omitempty"`
	
	// Analysis results
	IsAnalyzed   bool                `json:"isAnalyzed"`
	ValueScore   float64             `json:"valueScore"`
	AnalysisTime time.Time           `json:"analysisTime,omitempty"`
	AnalysisResults *AnalysisResults `json:"analysisResults,omitempty"`
	
	// Processing status
	Status       FileStatus          `json:"status"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
	CreatedAt    metav1.Time         `json:"createdAt"`
	UpdatedAt    metav1.Time         `json:"updatedAt"`
}

type AnalysisResults struct {
	StackTrace      string            `json:"stackTrace"`
	CrashReason     string            `json:"crashReason"`
	CrashAddress    string            `json:"crashAddress"`
	ThreadCount     int               `json:"threadCount"`
	LibraryVersions map[string]string `json:"libraryVersions"`
	MemoryInfo      MemoryInfo        `json:"memoryInfo"`
	RegisterInfo    map[string]string `json:"registerInfo"`
	SharedLibraries []string          `json:"sharedLibraries"`
	
	// AI Analysis Results
	AIAnalysis      *AIAnalysisResult `json:"aiAnalysis,omitempty"`
}

type AIAnalysisResult struct {
	Enabled          bool              `json:"enabled"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model"`
	AnalysisTime     time.Time         `json:"analysisTime"`
	Summary          string            `json:"summary"`
	RootCause        string            `json:"rootCause"`
	Impact           string            `json:"impact"`
	Recommendations  []string          `json:"recommendations"`
	Confidence       float64           `json:"confidence"`      // 0-1, AI's confidence in the analysis
	TokensUsed       int               `json:"tokensUsed"`
	CostUSD          float64           `json:"costUsd"`
	ErrorMessage     string            `json:"errorMessage,omitempty"`
	RelatedIssues    []string          `json:"relatedIssues,omitempty"`    // Known similar issues
	CodeSuggestions  []CodeSuggestion  `json:"codeSuggestions,omitempty"`  // Specific code fixes
}

type CodeSuggestion struct {
	File        string `json:"file"`
	Function    string `json:"function"`
	LineNumber  int    `json:"lineNumber,omitempty"`
	Issue       string `json:"issue"`
	Suggestion  string `json:"suggestion"`
	Priority    string `json:"priority"`  // "high", "medium", "low"
}

type MemoryInfo struct {
	VirtualSize  int64 `json:"virtualSize"`
	ResidentSize int64 `json:"residentSize"`
	HeapSize     int64 `json:"heapSize"`
	StackSize    int64 `json:"stackSize"`
}

type FileStatus string

const (
	StatusDiscovered FileStatus = "discovered"
	StatusProcessing FileStatus = "processing" 
	StatusAnalyzed   FileStatus = "analyzed"
	StatusStored     FileStatus = "stored"
	StatusSkipped    FileStatus = "skipped"
	StatusError      FileStatus = "error"
)

type CollectionEvent struct {
	Type         EventType           `json:"type"`
	CoredumpFile *CoredumpFile       `json:"coredumpFile,omitempty"`
	RestartEvent *discovery.RestartEvent `json:"restartEvent,omitempty"`
	Error        string              `json:"error,omitempty"`
	Timestamp    time.Time           `json:"timestamp"`
}

type EventType string

const (
	EventTypeFileDiscovered EventType = "file_discovered"
	EventTypeFileProcessed  EventType = "file_processed"
	EventTypeFileSkipped    EventType = "file_skipped"
	EventTypeFileError      EventType = "file_error"
	EventTypeRestartDetected EventType = "restart_detected"
)