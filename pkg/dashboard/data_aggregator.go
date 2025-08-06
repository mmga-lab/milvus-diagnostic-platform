package dashboard

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/klog/v2"
	
	"milvus-diagnostic-platform/pkg/analyzer"
	"milvus-diagnostic-platform/pkg/cleaner"
	"milvus-diagnostic-platform/pkg/collector"
	"milvus-diagnostic-platform/pkg/database"
	"milvus-diagnostic-platform/pkg/discovery"
	"milvus-diagnostic-platform/pkg/storage"
)

// DataAggregator 聚合来自各个组件的数据
type DataAggregator struct {
	mu                sync.RWMutex
	
	// 组件引用
	discoveryMgr      *discovery.Discovery
	collectorMgr      *collector.Collector
	analyzerMgr       *analyzer.Analyzer
	storageMgr        *storage.Storage
	cleanerMgr        *cleaner.Cleaner
	
	// 数据库存储
	coredumpStore     *database.CoredumpStore
	instanceStore     *database.InstanceStore
	storageStore      *database.StorageStore
	
	// 缓存的数据
	instances         map[string]*discovery.MilvusInstance
	coredumpFiles     map[string]*collector.CoredumpFile
	lastUpdate        time.Time
	
	// 统计数据
	stats             DashboardStats
}

type DashboardStats struct {
	CoredumpsDiscovered    int
	CoredumpsProcessed     int
	CoredumpsStored        int
	CoredumpsSkipped       int
	InstancesUninstalled   int
	AIAnalysisRequests     int
	AIAnalysisSuccessful   int
	LastProcessedTime      time.Time
	TotalStorageSize       int64
}

func NewDataAggregator(
	discoveryMgr *discovery.Discovery,
	collectorMgr *collector.Collector,
	analyzerMgr *analyzer.Analyzer,
	storageMgr *storage.Storage,
	cleanerMgr *cleaner.Cleaner,
	db *database.Database,
) *DataAggregator {
	return &DataAggregator{
		discoveryMgr:  discoveryMgr,
		collectorMgr:  collectorMgr,
		analyzerMgr:   analyzerMgr,
		storageMgr:    storageMgr,
		cleanerMgr:    cleanerMgr,
		coredumpStore: database.NewCoredumpStore(db),
		instanceStore: database.NewInstanceStore(db),
		storageStore:  database.NewStorageStore(db),
		instances:     make(map[string]*discovery.MilvusInstance),
		coredumpFiles: make(map[string]*collector.CoredumpFile),
		lastUpdate:    time.Now(),
	}
}

func (da *DataAggregator) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 初始数据加载
	da.refreshData()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			da.refreshData()
		}
	}
}

func (da *DataAggregator) refreshData() {
	da.mu.Lock()
	defer da.mu.Unlock()

	ctx := context.Background()
	klog.V(4).Info("Refreshing dashboard data")

	// 从数据库加载实例信息
	if instances, err := da.instanceStore.LoadInstances(ctx); err == nil {
		da.instances = make(map[string]*discovery.MilvusInstance)
		for _, instance := range instances {
			key := fmt.Sprintf("%s/%s", instance.Namespace, instance.Name)
			da.instances[key] = instance
		}
	}

	// 从数据库加载coredump文件信息
	if coredumps, err := da.coredumpStore.LoadCoredumpFiles(ctx); err == nil {
		da.coredumpFiles = make(map[string]*collector.CoredumpFile)
		for _, file := range coredumps {
			da.coredumpFiles[file.Path] = file
		}
	}

	// 同时从内存中的manager获取最新数据
	if da.discoveryMgr != nil {
		if instances := da.discoveryMgr.GetDiscoveredInstances(); instances != nil {
			for _, instance := range instances {
				key := fmt.Sprintf("%s/%s", instance.Namespace, instance.Name)
				da.instances[key] = instance
				// 保存到数据库
				da.instanceStore.SaveInstance(ctx, instance)
			}
		}
	}

	if da.collectorMgr != nil {
		if coredumps := da.collectorMgr.GetProcessedFiles(); coredumps != nil {
			for _, file := range coredumps {
				da.coredumpFiles[file.Path] = file
				// 保存到数据库
				da.coredumpStore.SaveCoredumpFile(ctx, file)
			}
		}
	}

	// 更新统计数据
	da.updateStats()
	da.lastUpdate = time.Now()
}

func (da *DataAggregator) updateStats() {
	// 重置统计数据
	da.stats = DashboardStats{}
	
	// 统计coredump文件
	for _, file := range da.coredumpFiles {
		da.stats.CoredumpsDiscovered++
		
		switch file.Status {
		case collector.StatusAnalyzed:
			da.stats.CoredumpsProcessed++
		case collector.StatusStored:
			da.stats.CoredumpsStored++
		case collector.StatusSkipped:
			da.stats.CoredumpsSkipped++
		}
		
		if file.AnalysisResults != nil && file.AnalysisResults.AIAnalysis != nil {
			da.stats.AIAnalysisRequests++
			if file.AnalysisResults.AIAnalysis.Summary != "" {
				da.stats.AIAnalysisSuccessful++
			}
		}
		
		if !file.AnalysisTime.IsZero() &&
			file.AnalysisTime.After(da.stats.LastProcessedTime) {
			da.stats.LastProcessedTime = file.AnalysisTime
		}
	}
}

func (da *DataAggregator) GetSummary() DashboardSummary {
	ctx := context.Background()

	// Get instance count from database
	instanceCount, _ := da.instanceStore.GetInstanceCount(ctx)
	
	// Get coredump counts from database  
	processedToday, _ := da.coredumpStore.GetTodayProcessedCount(ctx)
	highValueCount, _ := da.coredumpStore.GetHighValueCount(ctx)

	// Check if AI analysis is enabled
	aiAnalysisEnabled := false
	for _, file := range da.coredumpFiles {
		if file.AnalysisResults != nil && file.AnalysisResults.AIAnalysis != nil {
			aiAnalysisEnabled = true
			break
		}
	}

	return DashboardSummary{
		AgentStatus:        "running",
		MilvusInstances:    instanceCount,
		TotalCoredumps:     len(da.coredumpFiles),
		ProcessedToday:     processedToday,
		HighValueCoredumps: highValueCount,
		CleanedInstances:   da.stats.InstancesUninstalled,
		AIAnalysisEnabled:  aiAnalysisEnabled,
		LastUpdated:        da.lastUpdate,
	}
}

func (da *DataAggregator) GetInstances(params PaginationParams) PaginatedResponse {
	da.mu.RLock()
	defer da.mu.RUnlock()

	var overviews []InstanceOverview
	
	for _, instance := range da.instances {
		// 计算与此实例关联的coredump数量
		coredumpCount := 0
		recentRestarts := 0
		var lastActivity time.Time
		
		for _, file := range da.coredumpFiles {
			if file.InstanceName == instance.Name && file.PodNamespace == instance.Namespace {
				coredumpCount++
				if !file.CreatedAt.Time.IsZero() && file.CreatedAt.Time.After(lastActivity) {
					lastActivity = file.CreatedAt.Time
				}
			}
		}
		
		// 计算最近重启次数（24小时内）
		recent := time.Now().Add(-24 * time.Hour)
		for _, pod := range instance.Pods {
			if !pod.LastRestart.Time.IsZero() && pod.LastRestart.Time.After(recent) {
				recentRestarts++
			}
		}
		
		overview := InstanceOverview{
			Instance:       *instance,
			PodCount:       len(instance.Pods),
			CoredumpCount:  coredumpCount,
			RecentRestarts: recentRestarts,
			Status:         string(instance.Status),
			LastActivity:   lastActivity,
		}
		overviews = append(overviews, overview)
	}

	// 排序
	sort.Slice(overviews, func(i, j int) bool {
		switch params.SortBy {
		case "coredumps":
			if params.SortDesc {
				return overviews[i].CoredumpCount > overviews[j].CoredumpCount
			}
			return overviews[i].CoredumpCount < overviews[j].CoredumpCount
		case "restarts":
			if params.SortDesc {
				return overviews[i].RecentRestarts > overviews[j].RecentRestarts
			}
			return overviews[i].RecentRestarts < overviews[j].RecentRestarts
		default: // name
			if params.SortDesc {
				return overviews[i].Instance.Name > overviews[j].Instance.Name
			}
			return overviews[i].Instance.Name < overviews[j].Instance.Name
		}
	})

	// 分页
	total := len(overviews)
	start := (params.Page - 1) * params.PageSize
	end := start + params.PageSize
	
	if start >= total {
		overviews = []InstanceOverview{}
	} else {
		if end > total {
			end = total
		}
		overviews = overviews[start:end]
	}

	return PaginatedResponse{
		Data:       overviews,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: (total + params.PageSize - 1) / params.PageSize,
	}
}

func (da *DataAggregator) GetCoredumps(params PaginationParams) PaginatedResponse {
	da.mu.RLock()
	defer da.mu.RUnlock()

	var overviews []CoredumpOverview
	
	for _, file := range da.coredumpFiles {
		overview := CoredumpOverview{
			File:          *file,
			Instance:      file.InstanceName,
			Namespace:     file.PodNamespace,
			ValueScore:    file.ValueScore,
			HasAIAnalysis: file.AnalysisResults != nil && file.AnalysisResults.AIAnalysis != nil,
			StorageStatus: string(file.Status),
			CanView:       file.Status == collector.StatusStored,
		}
		overviews = append(overviews, overview)
	}

	// 排序
	sort.Slice(overviews, func(i, j int) bool {
		switch params.SortBy {
		case "score":
			if params.SortDesc {
				return overviews[i].ValueScore > overviews[j].ValueScore
			}
			return overviews[i].ValueScore < overviews[j].ValueScore
		case "size":
			if params.SortDesc {
				return overviews[i].File.Size > overviews[j].File.Size
			}
			return overviews[i].File.Size < overviews[j].File.Size
		case "time":
			if params.SortDesc {
				return overviews[i].File.ModTime.After(overviews[j].File.ModTime)
			}
			return overviews[i].File.ModTime.Before(overviews[j].File.ModTime)
		default: // filename
			if params.SortDesc {
				return overviews[i].File.FileName > overviews[j].File.FileName
			}
			return overviews[i].File.FileName < overviews[j].File.FileName
		}
	})

	// 分页
	total := len(overviews)
	start := (params.Page - 1) * params.PageSize
	end := start + params.PageSize
	
	if start >= total {
		overviews = []CoredumpOverview{}
	} else {
		if end > total {
			end = total
		}
		overviews = overviews[start:end]
	}

	return PaginatedResponse{
		Data:       overviews,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: (total + params.PageSize - 1) / params.PageSize,
	}
}

func (da *DataAggregator) GetCoredumpDetail(path string) (*CoredumpDetail, error) {
	da.mu.RLock()
	defer da.mu.RUnlock()

	file, exists := da.coredumpFiles[path]
	if !exists {
		return nil, fmt.Errorf("coredump file not found: %s", path)
	}

	detail := &CoredumpDetail{
		File:         *file,
		ViewerStatus: "stopped",
	}

	// 构建评分详情
	if file.IsAnalyzed {
		detail.ScoreBreakdown = da.calculateScoreBreakdown(file)
	}

	// 如果有GDB输出
	if file.AnalysisResults != nil {
		detail.GDBOutput = file.AnalysisResults.StackTrace
	}

	return detail, nil
}

func (da *DataAggregator) calculateScoreBreakdown(file *collector.CoredumpFile) ScoreBreakdown {
	// 这里实现评分细分逻辑，与analyzer中的评分算法保持一致
	breakdown := ScoreBreakdown{
		BaseScore:  4.0,
		TotalScore: file.ValueScore,
	}
	
	// 根据分析结果计算各维度得分
	if file.AnalysisResults != nil {
		if file.AnalysisResults.CrashReason != "" {
			breakdown.CrashReasonScore = 2.0
		}
		if file.AnalysisResults.StackTrace != "" && len(file.AnalysisResults.StackTrace) > 100 {
			breakdown.StackTraceScore = 1.5
		}
		if file.AnalysisResults.ThreadCount > 1 {
			breakdown.ThreadScore = 0.5
		}
	}
	
	if file.PodName != "" {
		breakdown.PodAssocScore = 1.0
	}
	
	if file.Signal == 11 || file.Signal == 6 || file.Signal == 8 {
		breakdown.SignalScore = 1.0
	}
	
	if file.Size > 100*1024*1024 {
		breakdown.FileSizeScore = 0.5
	}
	
	if time.Since(file.ModTime) < time.Hour {
		breakdown.FreshnessScore = 0.5
	}
	
	return breakdown
}

func (da *DataAggregator) GetMetrics() MetricsData {
	da.mu.RLock()
	defer da.mu.RUnlock()

	// 构建趋势数据（最近24小时，每小时一个点）
	trends := da.buildTrends()
	
	// 构建分数分布数据
	scoreDistribution := da.buildScoreDistribution()

	return MetricsData{
		CoredumpStats: CoredumpStats{
			Discovered: da.stats.CoredumpsDiscovered,
			Processed:  da.stats.CoredumpsProcessed,
			Stored:     da.stats.CoredumpsStored,
			Skipped:    da.stats.CoredumpsSkipped,
		},
		InstanceStats: da.buildInstanceStats(),
		ProcessingTrends: trends,
		ScoreDistribution: scoreDistribution,
		AIAnalysisStats: AIAnalysisStats{
			Enabled: da.stats.AIAnalysisRequests > 0,
			TotalRequests: da.stats.AIAnalysisRequests,
			SuccessfulAnalyses: da.stats.AIAnalysisSuccessful,
			FailedAnalyses: da.stats.AIAnalysisRequests - da.stats.AIAnalysisSuccessful,
		},
	}
}

func (da *DataAggregator) buildTrends() []TrendPoint {
	var trends []TrendPoint
	now := time.Now()
	
	for i := 23; i >= 0; i-- {
		hour := now.Add(-time.Duration(i) * time.Hour)
		hourStart := hour.Truncate(time.Hour)
		hourEnd := hourStart.Add(time.Hour)
		
		count := 0
		for _, file := range da.coredumpFiles {
			if !file.CreatedAt.Time.IsZero() &&
				file.CreatedAt.Time.After(hourStart) &&
				file.CreatedAt.Time.Before(hourEnd) {
				count++
			}
		}
		
		trends = append(trends, TrendPoint{
			Timestamp: hourStart,
			Value:     count,
			Label:     hourStart.Format("15:04"),
		})
	}
	
	return trends
}

func (da *DataAggregator) buildScoreDistribution() []ScorePoint {
	distribution := map[string]int{
		"0-2":  0,
		"2-4":  0,
		"4-6":  0,
		"6-8":  0,
		"8-10": 0,
	}
	
	for _, file := range da.coredumpFiles {
		score := file.ValueScore
		switch {
		case score < 2:
			distribution["0-2"]++
		case score < 4:
			distribution["2-4"]++
		case score < 6:
			distribution["4-6"]++
		case score < 8:
			distribution["6-8"]++
		default:
			distribution["8-10"]++
		}
	}
	
	var result []ScorePoint
	for range_, count := range distribution {
		result = append(result, ScorePoint{
			ScoreRange: range_,
			Count:      count,
		})
	}
	
	return result
}

func (da *DataAggregator) buildInstanceStats() InstanceStats {
	stats := InstanceStats{}
	
	for _, instance := range da.instances {
		switch instance.Type {
		case discovery.DeploymentTypeHelm:
			stats.Helm++
		case discovery.DeploymentTypeOperator:
			stats.Operator++
		}
		
		switch instance.Status {
		case discovery.InstanceStatusRunning:
			stats.Running++
		case discovery.InstanceStatusFailed:
			stats.Failed++
		case discovery.InstanceStatusTerminating:
			stats.Terminated++
		}
	}
	
	return stats
}