package dashboard

import (
	"time"
	
	"milvus-diagnostic-platform/pkg/discovery"
	"milvus-diagnostic-platform/pkg/collector"
)

// API响应数据结构
type DashboardSummary struct {
	AgentStatus        string                 `json:"agentStatus"`
	MilvusInstances    int                    `json:"milvusInstances"`
	TotalCoredumps     int                    `json:"totalCoredumps"`
	ProcessedToday     int                    `json:"processedToday"`
	HighValueCoredumps int                    `json:"highValueCoredumps"`
	CleanedInstances   int                    `json:"cleanedInstances"`
	AIAnalysisEnabled  bool                   `json:"aiAnalysisEnabled"`
	LastUpdated        time.Time              `json:"lastUpdated"`
}

type InstanceOverview struct {
	Instance    discovery.MilvusInstance `json:"instance"`
	PodCount    int                      `json:"podCount"`
	CoredumpCount int                    `json:"coredumpCount"`
	RecentRestarts int                   `json:"recentRestarts"`
	Status      string                   `json:"status"`
	LastActivity time.Time               `json:"lastActivity"`
}

type CoredumpOverview struct {
	File         collector.CoredumpFile `json:"file"`
	Instance     string                 `json:"instance"`
	Namespace    string                 `json:"namespace"`
	ValueScore   float64                `json:"valueScore"`
	HasAIAnalysis bool                  `json:"hasAiAnalysis"`
	StorageStatus string                `json:"storageStatus"`
	CanView      bool                   `json:"canView"`
}

type CoredumpDetail struct {
	File           collector.CoredumpFile `json:"file"`
	GDBOutput      string                 `json:"gdbOutput,omitempty"`
	ScoreBreakdown ScoreBreakdown         `json:"scoreBreakdown"`
	ViewerEndpoint string                 `json:"viewerEndpoint,omitempty"`
	ViewerStatus   string                 `json:"viewerStatus"`
}

type ScoreBreakdown struct {
	BaseScore         float64 `json:"baseScore"`
	CrashReasonScore  float64 `json:"crashReasonScore"`
	PanicKeywordScore float64 `json:"panicKeywordScore"`
	StackTraceScore   float64 `json:"stackTraceScore"`
	ThreadScore       float64 `json:"threadScore"`
	PodAssocScore     float64 `json:"podAssocScore"`
	SignalScore       float64 `json:"signalScore"`
	FileSizeScore     float64 `json:"fileSizeScore"`
	FreshnessScore    float64 `json:"freshnessScore"`
	TotalScore        float64 `json:"totalScore"`
}

type ViewerRequest struct {
	CoredumpID string `json:"coredumpId"`
	Duration   int    `json:"duration"` // 查看器保持活跃的分钟数
}

type ViewerResponse struct {
	ViewerID     string    `json:"viewerId"`
	PodName      string    `json:"podName"`
	Namespace    string    `json:"namespace"`
	WebTermURL   string    `json:"webTermUrl"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Status       string    `json:"status"`
}

type MetricsData struct {
	CoredumpStats      CoredumpStats    `json:"coredumpStats"`
	InstanceStats      InstanceStats    `json:"instanceStats"`
	ProcessingTrends   []TrendPoint     `json:"processingTrends"`
	ScoreDistribution  []ScorePoint     `json:"scoreDistribution"`
	AIAnalysisStats    AIAnalysisStats  `json:"aiAnalysisStats"`
}

type CoredumpStats struct {
	Discovered int `json:"discovered"`
	Processed  int `json:"processed"`
	Stored     int `json:"stored"`
	Skipped    int `json:"skipped"`
	Errors     int `json:"errors"`
}

type InstanceStats struct {
	Helm         int `json:"helm"`
	Operator     int `json:"operator"`
	Running      int `json:"running"`
	Failed       int `json:"failed"`
	Terminated   int `json:"terminated"`
}

type TrendPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     int       `json:"value"`
	Label     string    `json:"label"`
}

type ScorePoint struct {
	ScoreRange string `json:"scoreRange"`
	Count      int    `json:"count"`
}

type AIAnalysisStats struct {
	Enabled        bool    `json:"enabled"`
	TotalRequests  int     `json:"totalRequests"`
	SuccessfulAnalyses int `json:"successfulAnalyses"`
	FailedAnalyses int     `json:"failedAnalyses"`
	TotalCostUSD   float64 `json:"totalCostUsd"`
	AvgConfidence  float64 `json:"avgConfidence"`
}

// API错误响应
type APIError struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// 分页参数
type PaginationParams struct {
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	SortBy   string `json:"sortBy"`
	SortDesc bool   `json:"sortDesc"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	TotalPages int         `json:"totalPages"`
}