package monitor

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"milvus-coredump-agent/pkg/analyzer"
	"milvus-coredump-agent/pkg/cleaner"
	"milvus-coredump-agent/pkg/collector"
	"milvus-coredump-agent/pkg/config"
	"milvus-coredump-agent/pkg/storage"
)

type Monitor struct {
	config   *config.MonitorConfig
	registry *prometheus.Registry
	metrics  *Metrics
}

type Channels struct {
	CollectorEvents <-chan collector.CollectionEvent
	AnalyzerEvents  <-chan analyzer.AnalysisEvent
	StorageEvents   <-chan storage.StorageEvent
	CleanerEvents   <-chan cleaner.CleanupEvent
}

type Metrics struct {
	// Coredump collection metrics
	CoredumpsDiscovered prometheus.Counter
	CoredumpsProcessed  prometheus.Counter
	CoredumpsSkipped    prometheus.Counter
	CoredumpsErrors     prometheus.Counter
	
	// Analysis metrics
	AnalysisTotal        prometheus.Counter
	AnalysisSuccessful   prometheus.Counter
	AnalysisFailed       prometheus.Counter
	AnalysisDuration     prometheus.Histogram
	ValueScoreDistribution prometheus.Histogram
	
	// Storage metrics
	FilesStored          prometheus.Counter
	StorageSize          prometheus.Gauge
	StorageErrors        prometheus.Counter
	FilesDeleted         prometheus.Counter
	
	// Cleanup metrics
	InstancesUninstalled prometheus.Counter
	CleanupErrors        prometheus.Counter
	RestartCounts        *prometheus.GaugeVec
	
	// General metrics
	AgentUp              prometheus.Gauge
	MilvusInstancesTotal *prometheus.GaugeVec
	LastProcessedFile    prometheus.Gauge
}

func New(config *config.MonitorConfig) *Monitor {
	registry := prometheus.NewRegistry()
	
	metrics := &Metrics{
		CoredumpsDiscovered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_coredumps_discovered_total",
			Help: "Total number of coredump files discovered",
		}),
		CoredumpsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_coredumps_processed_total",
			Help: "Total number of coredump files processed",
		}),
		CoredumpsSkipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_coredumps_skipped_total",
			Help: "Total number of coredump files skipped",
		}),
		CoredumpsErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_coredumps_errors_total",
			Help: "Total number of coredump processing errors",
		}),
		AnalysisTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_analysis_total",
			Help: "Total number of coredump analyses performed",
		}),
		AnalysisSuccessful: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_analysis_successful_total",
			Help: "Total number of successful coredump analyses",
		}),
		AnalysisFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_analysis_failed_total",
			Help: "Total number of failed coredump analyses",
		}),
		AnalysisDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "milvus_coredump_agent_analysis_duration_seconds",
			Help:    "Duration of coredump analysis in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		}),
		ValueScoreDistribution: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "milvus_coredump_agent_value_score_distribution",
			Help:    "Distribution of coredump value scores",
			Buckets: prometheus.LinearBuckets(0, 1, 11),
		}),
		FilesStored: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_files_stored_total",
			Help: "Total number of coredump files stored",
		}),
		StorageSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "milvus_coredump_agent_storage_size_bytes",
			Help: "Current storage size in bytes",
		}),
		StorageErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_storage_errors_total",
			Help: "Total number of storage errors",
		}),
		FilesDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_files_deleted_total",
			Help: "Total number of files deleted during cleanup",
		}),
		InstancesUninstalled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_instances_uninstalled_total",
			Help: "Total number of Milvus instances uninstalled",
		}),
		CleanupErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "milvus_coredump_agent_cleanup_errors_total",
			Help: "Total number of cleanup errors",
		}),
		RestartCounts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "milvus_coredump_agent_restart_counts",
			Help: "Current restart counts for Milvus instances",
		}, []string{"instance", "namespace"}),
		AgentUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "milvus_coredump_agent_up",
			Help: "Whether the agent is up and running",
		}),
		MilvusInstancesTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "milvus_coredump_agent_milvus_instances_total",
			Help: "Total number of discovered Milvus instances",
		}, []string{"namespace", "type", "status"}),
		LastProcessedFile: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "milvus_coredump_agent_last_processed_file_timestamp",
			Help: "Timestamp of the last processed coredump file",
		}),
	}

	registry.MustRegister(
		metrics.CoredumpsDiscovered,
		metrics.CoredumpsProcessed,
		metrics.CoredumpsSkipped,
		metrics.CoredumpsErrors,
		metrics.AnalysisTotal,
		metrics.AnalysisSuccessful,
		metrics.AnalysisFailed,
		metrics.AnalysisDuration,
		metrics.ValueScoreDistribution,
		metrics.FilesStored,
		metrics.StorageSize,
		metrics.StorageErrors,
		metrics.FilesDeleted,
		metrics.InstancesUninstalled,
		metrics.CleanupErrors,
		metrics.RestartCounts,
		metrics.AgentUp,
		metrics.MilvusInstancesTotal,
		metrics.LastProcessedFile,
	)

	return &Monitor{
		config:   config,
		registry: registry,
		metrics:  metrics,
	}
}

func (m *Monitor) Start(ctx context.Context, channels *Channels) error {
	klog.Info("Starting monitoring system")

	m.metrics.AgentUp.Set(1)

	go m.processCollectorEvents(ctx, channels.CollectorEvents)
	go m.processAnalyzerEvents(ctx, channels.AnalyzerEvents)
	go m.processStorageEvents(ctx, channels.StorageEvents)
	go m.processCleanerEvents(ctx, channels.CleanerEvents)

	<-ctx.Done()
	m.metrics.AgentUp.Set(0)
	return nil
}

func (m *Monitor) GetHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Monitor) processCollectorEvents(ctx context.Context, events <-chan collector.CollectionEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			switch event.Type {
			case collector.EventTypeFileDiscovered:
				m.metrics.CoredumpsDiscovered.Inc()
				if event.CoredumpFile != nil {
					m.metrics.LastProcessedFile.SetToCurrentTime()
				}
			case collector.EventTypeFileProcessed:
				m.metrics.CoredumpsProcessed.Inc()
			case collector.EventTypeFileSkipped:
				m.metrics.CoredumpsSkipped.Inc()
			case collector.EventTypeFileError:
				m.metrics.CoredumpsErrors.Inc()
			}
		}
	}
}

func (m *Monitor) processAnalyzerEvents(ctx context.Context, events <-chan analyzer.AnalysisEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			m.metrics.AnalysisTotal.Inc()
			
			switch event.Type {
			case analyzer.EventTypeAnalysisComplete:
				m.metrics.AnalysisSuccessful.Inc()
				if event.CoredumpFile != nil && event.CoredumpFile.IsAnalyzed {
					m.metrics.ValueScoreDistribution.Observe(event.CoredumpFile.ValueScore)
					
					if !event.CoredumpFile.AnalysisTime.IsZero() {
						duration := event.CoredumpFile.AnalysisTime.Sub(event.CoredumpFile.CreatedAt.Time)
						m.metrics.AnalysisDuration.Observe(duration.Seconds())
					}
				}
			case analyzer.EventTypeAnalysisError:
				m.metrics.AnalysisFailed.Inc()
			}
		}
	}
}

func (m *Monitor) processStorageEvents(ctx context.Context, events <-chan storage.StorageEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			switch event.Type {
			case storage.EventTypeFileStored:
				m.metrics.FilesStored.Inc()
			case storage.EventTypeFileDeleted:
				m.metrics.FilesDeleted.Inc()
			case storage.EventTypeStorageError:
				m.metrics.StorageErrors.Inc()
			}
		}
	}
}

func (m *Monitor) processCleanerEvents(ctx context.Context, events <-chan cleaner.CleanupEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			switch event.Type {
			case cleaner.EventTypeInstanceUninstalled:
				m.metrics.InstancesUninstalled.Inc()
			case cleaner.EventTypeCleanupError:
				m.metrics.CleanupErrors.Inc()
			case cleaner.EventTypeRestartThreshold:
				m.metrics.RestartCounts.WithLabelValues(event.InstanceName, event.Namespace).Inc()
			}
		}
	}
}

func (m *Monitor) UpdateMilvusInstances(instances map[string]interface{}) {
	// This would be called periodically to update instance metrics
	// Implementation depends on the instance discovery structure
	klog.V(4).Infof("Updating Milvus instance metrics for %d instances", len(instances))
}