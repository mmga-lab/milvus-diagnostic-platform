-- Milvus Coredump Agent Database Schema

-- Milvus instances table
CREATE TABLE IF NOT EXISTS milvus_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    namespace TEXT NOT NULL,
    deployment_type TEXT NOT NULL, -- 'helm' or 'operator'
    labels TEXT, -- JSON encoded labels
    status TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Pods table (associated with instances)
CREATE TABLE IF NOT EXISTS pods (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    uid TEXT,
    restart_count INTEGER DEFAULT 0,
    last_restart DATETIME,
    status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES milvus_instances(id) ON DELETE CASCADE,
    UNIQUE(name, namespace)
);

-- Coredump files table
CREATE TABLE IF NOT EXISTS coredump_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT UNIQUE NOT NULL,
    file_name TEXT NOT NULL,
    size INTEGER NOT NULL,
    mod_time DATETIME,
    pid INTEGER,
    uid INTEGER,
    gid INTEGER,
    signal INTEGER,
    timestamp DATETIME,
    executable TEXT,
    arguments TEXT, -- JSON encoded array
    hostname TEXT,
    
    -- Associated pod information
    pod_name TEXT,
    pod_namespace TEXT,
    container_name TEXT,
    instance_name TEXT,
    
    -- Analysis results
    is_analyzed BOOLEAN DEFAULT FALSE,
    value_score REAL DEFAULT 0.0,
    analysis_time DATETIME,
    
    -- Processing status
    status TEXT DEFAULT 'discovered',
    error_message TEXT,
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Analysis results table (detailed GDB analysis)
CREATE TABLE IF NOT EXISTS analysis_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coredump_file_id INTEGER NOT NULL,
    stack_trace TEXT,
    crash_reason TEXT,
    crash_address TEXT,
    thread_count INTEGER,
    library_versions TEXT, -- JSON encoded map
    memory_info TEXT, -- JSON encoded memory info
    register_info TEXT, -- JSON encoded register map
    shared_libraries TEXT, -- JSON encoded array
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (coredump_file_id) REFERENCES coredump_files(id) ON DELETE CASCADE
);

-- AI analysis results table
CREATE TABLE IF NOT EXISTS ai_analysis_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coredump_file_id INTEGER NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    provider TEXT,
    model TEXT,
    analysis_time DATETIME,
    summary TEXT,
    root_cause TEXT,
    impact TEXT,
    recommendations TEXT, -- JSON encoded array
    confidence REAL,
    tokens_used INTEGER,
    cost_usd REAL,
    error_message TEXT,
    related_issues TEXT, -- JSON encoded array
    code_suggestions TEXT, -- JSON encoded array
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (coredump_file_id) REFERENCES coredump_files(id) ON DELETE CASCADE
);

-- Restart events table
CREATE TABLE IF NOT EXISTS restart_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pod_name TEXT NOT NULL,
    pod_namespace TEXT NOT NULL,
    pod_uid TEXT,
    container_name TEXT,
    restart_time DATETIME NOT NULL,
    exit_code INTEGER,
    is_panic BOOLEAN DEFAULT FALSE,
    reason TEXT,
    message TEXT,
    instance_name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Storage events table (tracking file storage operations)
CREATE TABLE IF NOT EXISTS storage_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coredump_file_id INTEGER,
    event_type TEXT NOT NULL, -- 'stored', 'deleted', 'error', 'cleanup'
    backend_type TEXT, -- 'local', 's3', 'nfs'
    storage_path TEXT,
    file_size INTEGER,
    compressed_size INTEGER,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (coredump_file_id) REFERENCES coredump_files(id) ON DELETE SET NULL
);

-- Cleanup events table (tracking instance cleanup operations)
CREATE TABLE IF NOT EXISTS cleanup_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    cleanup_type TEXT NOT NULL, -- 'helm_uninstall', 'operator_delete'
    restart_count INTEGER,
    reason TEXT,
    success BOOLEAN DEFAULT FALSE,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- System stats table (for dashboard metrics)
CREATE TABLE IF NOT EXISTS system_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    stat_name TEXT UNIQUE NOT NULL,
    stat_value TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_coredump_files_analysis_time ON coredump_files(analysis_time);
CREATE INDEX IF NOT EXISTS idx_coredump_files_value_score ON coredump_files(value_score);
CREATE INDEX IF NOT EXISTS idx_coredump_files_status ON coredump_files(status);
CREATE INDEX IF NOT EXISTS idx_coredump_files_instance_name ON coredump_files(instance_name);
CREATE INDEX IF NOT EXISTS idx_restart_events_pod ON restart_events(pod_name, pod_namespace);
CREATE INDEX IF NOT EXISTS idx_restart_events_time ON restart_events(restart_time);
CREATE INDEX IF NOT EXISTS idx_pods_instance_id ON pods(instance_id);
CREATE INDEX IF NOT EXISTS idx_storage_events_file_id ON storage_events(coredump_file_id);
CREATE INDEX IF NOT EXISTS idx_ai_analysis_file_id ON ai_analysis_results(coredump_file_id);
CREATE INDEX IF NOT EXISTS idx_analysis_results_file_id ON analysis_results(coredump_file_id);

-- Log entries table (for log collection and analysis)
CREATE TABLE IF NOT EXISTS log_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_file TEXT NOT NULL,
    line_number INTEGER,
    timestamp DATETIME NOT NULL,
    level TEXT, -- 'ERROR', 'WARN', 'INFO', 'DEBUG'
    component TEXT, -- pod/container name or component
    namespace TEXT,
    message TEXT NOT NULL,
    raw_line TEXT, -- original log line
    
    -- Pattern matching results
    is_error BOOLEAN DEFAULT FALSE,
    is_warning BOOLEAN DEFAULT FALSE,
    error_pattern TEXT, -- which pattern matched
    
    -- Associated instance information
    instance_name TEXT,
    pod_name TEXT,
    container_name TEXT,
    
    -- Analysis results
    is_analyzed BOOLEAN DEFAULT FALSE,
    severity_score REAL DEFAULT 0.0, -- 0-10 scale
    analysis_time DATETIME,
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- System metrics table (for performance monitoring)
CREATE TABLE IF NOT EXISTS system_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_name TEXT NOT NULL,
    metric_type TEXT NOT NULL, -- 'counter', 'gauge', 'histogram'
    value REAL NOT NULL,
    labels TEXT, -- JSON encoded labels map
    timestamp DATETIME NOT NULL,
    source TEXT, -- 'system', 'application', 'kubernetes', 'custom'
    
    -- Associated instance information
    node_name TEXT,
    namespace TEXT,
    pod_name TEXT,
    container_name TEXT,
    instance_name TEXT,
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Application metrics table (for Milvus-specific metrics)
CREATE TABLE IF NOT EXISTS application_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value REAL NOT NULL,
    metric_labels TEXT, -- JSON encoded labels
    endpoint TEXT, -- metrics endpoint URL
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Health check results table
CREATE TABLE IF NOT EXISTS health_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_name TEXT NOT NULL,
    check_type TEXT NOT NULL, -- 'endpoint', 'service', 'dependency'
    target TEXT NOT NULL, -- URL, service name, etc.
    status TEXT NOT NULL, -- 'healthy', 'unhealthy', 'unknown'
    response_time_ms INTEGER,
    status_code INTEGER,
    error_message TEXT,
    
    -- Associated instance information
    instance_name TEXT,
    namespace TEXT,
    pod_name TEXT,
    
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Reports table (for generated reports tracking)
CREATE TABLE IF NOT EXISTS reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id TEXT UNIQUE NOT NULL,
    report_type TEXT NOT NULL, -- 'daily', 'weekly', 'monthly', 'adhoc'
    title TEXT NOT NULL,
    format TEXT NOT NULL, -- 'html', 'pdf', 'json'
    file_path TEXT,
    file_size INTEGER,
    
    -- Report content metadata
    period_start DATETIME NOT NULL,
    period_end DATETIME NOT NULL,
    included_metrics TEXT, -- JSON array of included metrics
    summary_data TEXT, -- JSON encoded summary data
    
    -- Generation status
    status TEXT DEFAULT 'generating', -- 'generating', 'completed', 'failed', 'delivered'
    generation_time_sec REAL,
    error_message TEXT,
    
    -- Delivery status
    delivered_at DATETIME,
    delivery_methods TEXT, -- JSON array of successful delivery methods
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Report sections table (for modular report content)
CREATE TABLE IF NOT EXISTS report_sections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL,
    section_name TEXT NOT NULL,
    section_order INTEGER NOT NULL,
    section_type TEXT NOT NULL, -- 'summary', 'chart', 'table', 'analysis'
    content TEXT, -- JSON encoded section content
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE
);

-- Correlation events table (for multi-dimensional analysis)
CREATE TABLE IF NOT EXISTS correlation_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL, -- 'restart', 'coredump', 'log_error', 'metric_spike'
    event_id INTEGER NOT NULL, -- ID in the respective table
    correlation_key TEXT NOT NULL, -- for grouping related events
    timestamp DATETIME NOT NULL,
    
    -- Event context
    instance_name TEXT,
    namespace TEXT,
    pod_name TEXT,
    container_name TEXT,
    
    -- Correlation metadata
    severity_level TEXT, -- 'low', 'medium', 'high', 'critical'
    tags TEXT, -- JSON array of tags for filtering
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Create additional indexes for new tables
CREATE INDEX IF NOT EXISTS idx_log_entries_timestamp ON log_entries(timestamp);
CREATE INDEX IF NOT EXISTS idx_log_entries_level ON log_entries(level);
CREATE INDEX IF NOT EXISTS idx_log_entries_instance ON log_entries(instance_name);
CREATE INDEX IF NOT EXISTS idx_log_entries_error ON log_entries(is_error);
CREATE INDEX IF NOT EXISTS idx_system_metrics_timestamp ON system_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_system_metrics_name ON system_metrics(metric_name);
CREATE INDEX IF NOT EXISTS idx_system_metrics_instance ON system_metrics(instance_name);
CREATE INDEX IF NOT EXISTS idx_application_metrics_instance ON application_metrics(instance_name);
CREATE INDEX IF NOT EXISTS idx_application_metrics_timestamp ON application_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_health_checks_timestamp ON health_checks(timestamp);
CREATE INDEX IF NOT EXISTS idx_health_checks_status ON health_checks(status);
CREATE INDEX IF NOT EXISTS idx_health_checks_instance ON health_checks(instance_name);
CREATE INDEX IF NOT EXISTS idx_reports_type ON reports(report_type);
CREATE INDEX IF NOT EXISTS idx_reports_period ON reports(period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status);
CREATE INDEX IF NOT EXISTS idx_correlation_events_key ON correlation_events(correlation_key);
CREATE INDEX IF NOT EXISTS idx_correlation_events_timestamp ON correlation_events(timestamp);

-- Insert initial system stats
INSERT OR REPLACE INTO system_stats (stat_name, stat_value) VALUES
('agent_start_time', datetime('now')),
('total_instances_discovered', '0'),
('total_coredumps_processed', '0'),
('total_ai_analysis_requests', '0'),
('total_instances_cleaned', '0'),
('total_log_entries', '0'),
('total_metrics_collected', '0'),
('total_reports_generated', '0'),
('last_report_generation', '');